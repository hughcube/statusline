// Package billing keeps a cached snapshot of a metered API's account balance
// for the status bar.
//
// Constraints:
//   - the foreground never performs network I/O: a cache hit is rendered as-is
//     and an expired entry only schedules a detached worker;
//   - entries are keyed by provider + a domain-separated hash of the
//     credential, so switching API keys can never mix balances and the key
//     itself is never part of a path, a cache line, or a diagnostic;
//   - a failed refresh keeps the last good balance and backs off instead of
//     spawning a worker on every frame.
package billing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hughcube/statusline/internal/agent"
	"github.com/hughcube/statusline/internal/pathx"
)

const (
	providerDeepSeek = "deepseek"
	officialBaseURL  = "https://api.deepseek.com"
	balanceEndpoint  = officialBaseURL + "/user/balance"

	// providerCodeBuddy is the shared CodeBuddy / WorkBuddy credits pool. It is
	// a different shape from a cash balance: one total plus a daily expiry
	// schedule, so it renders as one grouped segment instead of a label+amount.
	providerCodeBuddy       = "codebuddy"
	workBuddyBaseURL        = "https://www.workbuddy.cn"
	creditsSummaryEndpoint  = "/billing/meter/get-user-resource-summary"
	creditsPackagesEndpoint = "/billing/meter/get-user-resource-free-packages"
	creditsUserAgent        = "WorkBuddy/1.0.0 (Windows NT 10.0; Win64; x64)"
	// creditsWindow is the rolling look-ahead for "expiring soon".
	creditsWindow = 24 * time.Hour

	cacheSubdir  = "billing"
	cacheSuffix  = ".json"
	cacheVersion = 1

	// defaultTTL is the healthy refresh interval for both DeepSeek and CodeBuddy
	// (60s TTL, half-TTL soft refresh triggers asynchronously every 30s).
	// STATUSLINE_BILLING_TTL overrides it in seconds for all providers.
	defaultTTL = 60 * time.Second
	// minTTL prevents a typo from turning the bar into a request loop.
	minTTL = 30 * time.Second
	// errorRetry is the backoff after a failed refresh; it prevents a network
	// outage from spawning a worker on every rendered frame.
	errorRetry = 60 * time.Second
	// hardMaxAge is the minimum lifetime of a displayed balance. It grows with
	// a large TTL so "?" and "hidden" stay meaningful.
	hardMaxAge = 2 * time.Hour
	// lockTTL lets a new frame steal the refresh slot from a crashed worker.
	lockTTL = 10 * time.Second

	// requestTimeout is the whole-request budget. The endpoint answers in
	// ~135 ms; three seconds leaves a 20x margin while staying well under
	// lockTTL, so two workers can never overlap.
	requestTimeout = 3 * time.Second
	// maxBody bounds a hostile or broken response.
	maxBody = 1 << 20
)

// State is what the renderer needs, plus safe diagnostic fields. It never
// carries the credential.
type State struct {
	Provider  string
	Label     string // display name, e.g. "DeepSeek"
	Origin    string
	Amount    string
	Currency  string
	FetchedAt time.Time
	Stale     bool
	Err       string
	// Account and Expiring are only set by the credits provider: Account labels
	// the pool and Expiring is the amount that will be voided within 24 hours.
	// Both stay empty for balance-style providers.
	Account  string
	Expiring string
}

// providerLabel maps the provider id to the short display name used by the
// bar. Keeping it here means the renderer never needs provider knowledge.
func providerLabel(provider string) string {
	switch provider {
	case "deepseek":
		return "DeepSeek"
	case "kimi", "moonshot":
		return "Kimi"
	case "glm", "zhipu":
		return "GLM"
	case "bailian":
		return "Bailian"
	default:
		return provider
	}
}

// Disabled reports whether billing must not read credentials or spawn workers.
// Tests run with STATUSLINE_TESTING=1 and must be deterministic and offline.
func Disabled() bool {
	if os.Getenv("STATUSLINE_TESTING") == "1" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("STATUSLINE_BILLING"))) {
	case "off", "0", "false", "no":
		return true
	}
	return false
}

// effectiveTTL parses STATUSLINE_BILLING_TTL (seconds). Invalid or non-positive
// values fall back to the default; values below minTTL are clamped.
func effectiveTTL() time.Duration {
	raw := strings.TrimSpace(os.Getenv("STATUSLINE_BILLING_TTL"))
	if raw == "" {
		return defaultTTL
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultTTL
	}
	d := time.Duration(n) * time.Second
	if d < minTTL {
		return minTTL
	}
	return d
}

// effectiveTTLFor returns the effective TTL for the provider. Both DeepSeek and
// CodeBuddy default to defaultTTL (60s), and STATUSLINE_BILLING_TTL overrides
// them universally.
func effectiveTTLFor(provider string) time.Duration {
	return effectiveTTL()
}

// staleAfter is when the value gets a "?" marker.
func staleAfter(ttl time.Duration) time.Duration { return 2 * ttl }

// maxAge is when the value is hidden entirely.
func maxAge(ttl time.Duration) time.Duration {
	if d := 3 * ttl; d > hardMaxAge {
		return d
	}
	return hardMaxAge
}

// SourcesFor collects credentials the resolved agent can offer. Only Claude
// Code implements agent.Billable today; the interface is the extension point
// for other CLIs.
func SourcesFor(agentName string) []agent.BillingSource {
	a := agent.ByName(agentName)
	if a == nil {
		return nil
	}
	b, ok := a.(agent.Billable)
	if !ok {
		return nil
	}
	return b.BillingSources()
}

// pick selects the first usable source. It also pins the request URL to the
// official endpoint, so a configured path such as /anthropic can never leak a
// bearer token to an unexpected route.
func pick(srcs []agent.BillingSource) (agent.BillingSource, bool) {
	for _, s := range srcs {
		token := strings.TrimSpace(s.Token)
		if token == "" {
			continue
		}
		switch s.Provider {
		case providerDeepSeek:
			if !agent.IsOfficialDeepSeekURL(s.BaseURL) {
				continue
			}
			s.Token = token
			s.BaseURL = officialBaseURL
			return s, true
		case providerCodeBuddy:
			if !agent.IsOfficialWorkBuddyURL(s.BaseURL) {
				continue
			}
			s.Token = token
			s.BaseURL = workBuddyBaseURL
			return s, true
		}
	}
	return agent.BillingSource{}, false
}

// entryName is the only place a credential influences a path, and only as a
// domain-separated SHA-256 prefix. Sixteen hex characters (64 bits) make
// collisions negligible for a handful of keys; the name leaks neither the key
// nor any prefix/suffix of it.
func entryName(src agent.BillingSource) string {
	// Balance providers key on the credential because the key is the account.
	// Credits providers key on the uid when the credential names one, so the
	// 30-day token rotation keeps hitting the same cache file.
	identity := strings.TrimSpace(src.Token)
	if src.Provider == providerCodeBuddy && strings.TrimSpace(src.UID) != "" {
		identity = strings.TrimSpace(src.UID)
	}
	sum := sha256.Sum256([]byte("statusline:billing:" + src.Provider + ":" + identity))
	return src.Provider + "-" + hex.EncodeToString(sum[:8]) + cacheSuffix
}

// CachePath returns the per-key cache file:
//
//	<cache>/statusline/billing/deepseek-<16 hex>.json
//
// The same key used by two windows (or later by two agents) shares one file,
// which is correct because it is the same account. A different key always gets
// a different file.
func CachePath(cacheDir string, src agent.BillingSource) string {
	return filepath.Join(cacheDir, cacheSubdir, entryName(src))
}

// Load returns the cached snapshot for the first discoverable credential. It
// never blocks on the network and never returns an error to the caller. A miss
// or an expired entry schedules a detached worker and this frame renders the
// previous value immediately (or empty if none exists yet).
func Load(cacheDir, agentName string) State {
	if Disabled() {
		return State{}
	}
	src, ok := pick(SourcesFor(agentName))
	if !ok {
		return State{}
	}
	ttl := effectiveTTLFor(src.Provider)
	path := CachePath(cacheDir, src)
	e := readEntry(path)
	now := time.Now()
	if shouldRefresh(e, now, ttl) && acquireLock(path) {
		spawnWorker(agentName, path)
	}
	return stateFrom(e, src, now, ttl)
}

// shouldRefresh decides whether this frame may start a worker.
// To keep balance fresh without blocking, an async refresh is scheduled
// as soon as half the TTL has elapsed (soft expiry / early refresh).
// Failures back off for errorRetry (60s) to avoid spamming the endpoint.
func shouldRefresh(e entry, now time.Time, ttl time.Duration) bool {
	if e.lastAttempt.IsZero() {
		return true
	}
	wait := ttl / 2
	if e.status == statusErr {
		wait = errorRetry
	}
	return now.Sub(e.lastAttempt) >= wait
}

// stateFrom turns a cache entry into renderable state.
// Even if expired or offline, the cached value is always rendered so the status
// bar never suddenly blanks out, with Stale marked when beyond staleAfter(ttl).
func stateFrom(e entry, src agent.BillingSource, now time.Time, ttl time.Duration) State {
	st := State{
		Provider:  src.Provider,
		Label:     providerLabel(src.Provider),
		Origin:    src.Origin,
		FetchedAt: e.lastOK,
		Err:       e.err,
	}
	if e.lastOK.IsZero() {
		return st
	}
	age := now.Sub(e.lastOK)
	if src.Provider == providerCodeBuddy {
		c, ok := parseCredits(e.response, now)
		if !ok {
			if st.Err == "" {
				st.Err = "invalid credits payload"
			}
			return st
		}
		st.Account = e.account
		if st.Account == "" {
			st.Account = src.Account
		}
		st.Amount = formatCredits(c.Total)
		if c.Expiring > 0 {
			st.Expiring = formatCredits(c.Expiring)
		}
		st.Stale = age > staleAfter(ttl) || e.status == statusErr
		return st
	}
	bal, ok := parseBalance(e.response)
	if !ok {
		if st.Err == "" {
			st.Err = "invalid balance payload"
		}
		return st
	}
	st.Currency = bal.Currency
	st.Amount = formatAmount(bal.Currency, bal.Total, bal.Available)
	st.Stale = age > staleAfter(ttl) || e.status == statusErr
	return st
}

// formatAmount renders the number with the narrowest sensible currency mark.
// A non-available account gets a trailing "!" because the bar has no colour.
func formatAmount(currency, total string, available bool) string {
	total = strings.TrimSpace(total)
	if total == "" {
		return ""
	}
	if _, err := strconv.ParseFloat(total, 64); err != nil {
		return ""
	}
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "CNY":
		total = "¥" + total
	case "USD":
		total = "$" + total
	case "":
		// No currency reported; leave the number bare.
	default:
		total = total + " " + strings.ToUpper(strings.TrimSpace(currency))
	}
	if !available {
		total += "!"
	}
	return total
}

// ---- cache file -----------------------------------------------------------

const (
	statusOK  = "ok"
	statusErr = "err"
)

// entry is the in-memory view of the cache. response is the complete JSON
// body returned by DeepSeek, so fields we do not render yet (extra currencies,
// future schema additions) are never lost. It survives a failed refresh.
type entry struct {
	lastOK      time.Time
	lastAttempt time.Time
	status      string
	err         string
	response    json.RawMessage
	// account is the display name captured at refresh time, so an offline frame
	// can still label the number.
	account string
}

// cacheFile is the on-disk envelope. Version makes a future format change
// detectable instead of silently misreading old data.
type cacheFile struct {
	Version     int             `json:"version"`
	FetchedAt   int64           `json:"fetched_at"`
	LastAttempt int64           `json:"last_attempt"`
	Status      string          `json:"status"`
	Error       string          `json:"error,omitempty"`
	Response    json.RawMessage `json:"response,omitempty"`
	Account     string          `json:"account,omitempty"`
}

// readEntry parses the JSON envelope. Malformed input, an unknown version, or
// the old TSV format is treated as an empty entry, never as a balance.
func readEntry(path string) entry {
	b, err := os.ReadFile(path)
	if err != nil {
		return entry{}
	}
	var f cacheFile
	if json.Unmarshal(b, &f) != nil || f.Version != cacheVersion {
		return entry{}
	}
	return entry{
		lastOK:      unixTime(f.FetchedAt),
		lastAttempt: unixTime(f.LastAttempt),
		status:      f.Status,
		err:         f.Error,
		response:    f.Response,
		account:     f.Account,
	}
}

func writeEntry(path string, e entry) {
	f := cacheFile{
		Version:     cacheVersion,
		FetchedAt:   unix(e.lastOK),
		LastAttempt: unix(e.lastAttempt),
		Status:      e.status,
		Error:       e.err,
		Response:    e.response,
		Account:     e.account,
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return
	}
	b = append(b, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := fmt.Sprintf("%s.%d.%d.tmp", path, os.Getpid(), time.Now().UnixNano())
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}

func unix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func unixTime(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

// ---- refresh worker -------------------------------------------------------

// CacheWorker is the entry point of the internal __billingcache subcommand.
// The credential is rediscovered from the inherited environment; it is never
// passed on the command line.
func CacheWorker(agentName, cachePath string) {
	defer releaseLock(cachePath)

	// Persist the attempt before any network work: if this process is killed
	// mid-request, the next frame still backs off instead of retrying.
	markAttempt(cachePath)

	src, ok := pick(SourcesFor(agentName))
	if !ok {
		writeFailure(cachePath, "no usable DeepSeek credential")
		return
	}
	// The credential may have changed between spawn and discovery (user
	// switched accounts). Never write one key's balance into another key's
	// file: the file name is the account boundary.
	if filepath.Base(cachePath) != entryName(src) {
		writeFailure(cachePath, "credential changed during refresh")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	if src.Provider == providerCodeBuddy {
		raw, err := fetchCodeBuddy(ctx, src.BaseURL, src.Token)
		if err != nil {
			writeFailure(cachePath, err.Error())
			return
		}
		now := time.Now()
		writeEntry(cachePath, entry{
			lastOK:      now,
			lastAttempt: now,
			status:      statusOK,
			response:    raw,
			account:     src.Account,
		})
		return
	}

	_, raw, err := fetchDeepSeek(ctx, balanceEndpoint, src.Token)
	if err != nil {
		writeFailure(cachePath, err.Error())
		return
	}
	now := time.Now()
	writeEntry(cachePath, entry{
		lastOK:      now,
		lastAttempt: now,
		status:      statusOK,
		response:    raw,
	})
}

// markAttempt records the attempt without touching the last good value or its
// status, so a crash leaves a cache entry that is not immediately retried.
func markAttempt(path string) {
	e := readEntry(path)
	e.lastAttempt = time.Now()
	writeEntry(path, e)
}

// writeFailure keeps the previous good balance and records the attempt so the
// next frame backs off instead of retrying immediately.
func writeFailure(path, msg string) {
	e := readEntry(path)
	e.lastAttempt = time.Now()
	e.status = statusErr
	e.err = msg
	writeEntry(path, e)
}

// spawnWorker starts a detached copy of this binary. The status bar returns
// immediately; the child does the network I/O and writes the cache.
func spawnWorker(agentName, path string) {
	exe, err := os.Executable()
	if err != nil {
		releaseLock(path)
		writeFailure(path, "cannot locate executable")
		return
	}
	cmd := exec.Command(exe, "__billingcache", agentName, path)
	pathx.DetachCmd(cmd)
	if err := cmd.Start(); err != nil {
		releaseLock(path)
		writeFailure(path, "cannot start refresh worker")
		return
	}
	_ = cmd.Process.Release()
}

// acquireLock claims the refresh slot for one key. The lock is a directory so
// creation is atomic; a stale lock is stolen after lockTTL.
func acquireLock(path string) bool {
	lock := path + ".lock"
	if fi, err := os.Stat(lock); err == nil {
		if time.Since(fi.ModTime()) < lockTTL {
			return false
		}
		_ = os.RemoveAll(lock)
	}
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		return false
	}
	return os.Mkdir(lock, 0o755) == nil
}

func releaseLock(path string) {
	_ = os.RemoveAll(path + ".lock")
}

// ---- HTTP -----------------------------------------------------------------

// balance is the subset of GET /user/balance the bar uses. DeepSeek exposes
// exactly these fields; there is no public cost/usage endpoint.
type balance struct {
	Currency  string
	Total     string
	ToppedUp  string
	Granted   string
	Available bool
}

type balanceResponse struct {
	IsAvailable  *bool `json:"is_available"`
	BalanceInfos []struct {
		Currency string `json:"currency"`
		Total    string `json:"total_balance"`
		ToppedUp string `json:"topped_up_balance"`
		Granted  string `json:"granted_balance"`
	} `json:"balance_infos"`
}

var httpClient = &http.Client{
	Timeout: requestTimeout,
	// A redirect is never expected; refusing it guarantees the bearer token is
	// only sent to the URL we constructed.
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func fetchDeepSeek(ctx context.Context, endpoint, token string) (balance, json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return balance{}, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return balance{}, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return balance{}, nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return balance{}, nil, err
	}
	bal, ok := parseBalance(b)
	if !ok {
		return balance{}, nil, fmt.Errorf("invalid balance payload")
	}
	// Keep the complete response, not just the fields the bar renders today.
	return bal, json.RawMessage(append([]byte(nil), b...)), nil
}

// parseBalance extracts the fields the bar currently shows while leaving the
// rest of the response untouched in the cache.
func parseBalance(raw []byte) (balance, bool) {
	if len(raw) == 0 {
		return balance{}, false
	}
	var r balanceResponse
	if json.Unmarshal(raw, &r) != nil || len(r.BalanceInfos) == 0 {
		return balance{}, false
	}
	info := r.BalanceInfos[0]
	available := true
	if r.IsAvailable != nil {
		available = *r.IsAvailable
	}
	return balance{
		Currency:  info.Currency,
		Total:     info.Total,
		ToppedUp:  info.ToppedUp,
		Granted:   info.Granted,
		Available: available,
	}, true
}

// ---- CodeBuddy credits ----------------------------------------------------

// credits is the render-time view of one pool.
type credits struct {
	Total    float64
	Expiring float64
}

// postJSON performs one POST with the billing gateway headers. The gateway
// answers 403 to a default client user agent, hence the explicit one.
func postJSON(ctx context.Context, endpoint, token string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", creditsUserAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBody))
}

// creditsSummaryBody is the subset of the summary response the bar needs.
type creditsSummaryBody struct {
	Code int `json:"code"`
	Data struct {
		TotalCredits json.RawMessage `json:"TotalCredits"`
		Packages     []struct {
			PackageCode         string          `json:"PackageCode"`
			CycleRemainCapacity json.RawMessage `json:"CycleRemainCapacity"`
		} `json:"Packages"`
	} `json:"data"`
}

// creditsPackagesBody is the subset of the free-packages response: only the
// cycle window and the remaining amount matter for the 24h figure.
type creditsPackagesBody struct {
	Code int `json:"code"`
	Data struct {
		Accounts []struct {
			Status              int             `json:"Status"`
			CycleCapacityRemain json.RawMessage `json:"CycleCapacityRemain"`
			CycleEndTime        string          `json:"CycleEndTime"`
		} `json:"Accounts"`
	} `json:"data"`
}

// creditsEnvelope is what the cache stores: the summary body verbatim (so
// fields the bar does not render yet survive a refresh) plus the expiry
// schedule flattened to [unix_seconds, remaining] pairs.
//
// The package detail is deliberately NOT cached verbatim: it is ~80 KB of
// resource bookkeeping, and reading that per frame costs more than the whole
// rest of the render.
type creditsEnvelope struct {
	Summary json.RawMessage `json:"summary"`
	Expiry  [][2]float64    `json:"expiry,omitempty"`
}

// fetchCodeBuddy performs the two queries the segment needs. The package detail
// is best effort: losing it only drops the 24h figure, never the total.
func fetchCodeBuddy(ctx context.Context, base, token string) (json.RawMessage, error) {
	sumRaw, err := postJSON(ctx, base+creditsSummaryEndpoint, token, []byte("{}"))
	if err != nil {
		return nil, err
	}
	var sum creditsSummaryBody
	if json.Unmarshal(sumRaw, &sum) != nil {
		return nil, fmt.Errorf("invalid credits summary payload")
	}
	if sum.Code != 0 {
		return nil, fmt.Errorf("credits summary code %d", sum.Code)
	}

	out := creditsEnvelope{Summary: sumRaw}
	codes := make([]string, 0, len(sum.Data.Packages))
	for _, p := range sum.Data.Packages {
		if p.PackageCode != "" {
			codes = append(codes, p.PackageCode)
		}
	}
	if len(codes) > 0 {
		body, _ := json.Marshal(map[string]any{
			"PackageCodes": codes,
			"pageNumber":   1,
			"pageSize":     50,
		})
		if pkgRaw, err := postJSON(ctx, base+creditsPackagesEndpoint, token, body); err == nil {
			out.Expiry = expiryPoints(pkgRaw)
		}
	}
	return json.Marshal(out)
}

// parseCredits turns a cached envelope into the two numbers the bar shows. The
// 24h figure is computed against now rather than fetch time, so a cache entry
// that survived a failed refresh cannot claim credits that already expired.
func parseCredits(raw json.RawMessage, now time.Time) (credits, bool) {
	var env creditsEnvelope
	if len(raw) == 0 || json.Unmarshal(raw, &env) != nil || len(env.Summary) == 0 {
		return credits{}, false
	}
	var sum creditsSummaryBody
	if json.Unmarshal(env.Summary, &sum) != nil || sum.Code != 0 {
		return credits{}, false
	}

	out := credits{Total: toFloat(sum.Data.TotalCredits)}
	if out.Total == 0 {
		for _, p := range sum.Data.Packages {
			out.Total += toFloat(p.CycleRemainCapacity)
		}
	}

	deadline := float64(now.Add(creditsWindow).Unix())
	nowUnix := float64(now.Unix())
	for _, p := range env.Expiry {
		if p[1] <= 0 {
			continue
		}
		if p[0] > nowUnix && p[0] <= deadline {
			out.Expiring += p[1]
		}
	}
	return out, true
}

// expiryPoints flattens the package detail into the only pairs the bar needs:
// when a batch expires and how much of it is left.
func expiryPoints(raw json.RawMessage) [][2]float64 {
	var body creditsPackagesBody
	if json.Unmarshal(raw, &body) != nil || body.Code != 0 {
		return nil
	}
	points := make([][2]float64, 0, len(body.Data.Accounts))
	for _, a := range body.Data.Accounts {
		remain := toFloat(a.CycleCapacityRemain)
		if a.Status != 0 || remain <= 0 {
			continue
		}
		end, err := time.ParseInLocation("2006-01-02 15:04:05", a.CycleEndTime, time.Local)
		if err != nil {
			continue
		}
		points = append(points, [2]float64{float64(end.Unix()), remain})
	}
	return points
}

// toFloat accepts the numeric shapes this API mixes: JSON numbers and strings.
func toFloat(raw json.RawMessage) float64 {
	if len(raw) == 0 {
		return 0
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case string:
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return f
		}
	}
	return 0
}

// formatCredits rounds to a whole credit and groups thousands: 3359.22 -> 3,359.
func formatCredits(v float64) string {
	n := int64(math.Round(v))
	if n < 0 {
		n = 0
	}
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
