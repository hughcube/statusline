package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hughcube/statusline/internal/agent"
)

// testResponse builds a complete DeepSeek /user/balance body. extra is
// appended as additional top-level fields.
func testResponse(currency, total string, available bool, extra string) json.RawMessage {
	av := "true"
	if !available {
		av = "false"
	}
	if extra != "" {
		extra = "," + extra
	}
	return json.RawMessage(fmt.Sprintf(
		`{"is_available":%s,"balance_infos":[{"currency":%q,"total_balance":%q,"granted_balance":"0.00","topped_up_balance":%q}]%s}`,
		av, currency, total, total, extra))
}

func TestEffectiveTTL(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Duration
	}{
		{"", defaultTTL},
		{"60", 60 * time.Second},
		{"30", 30 * time.Second},
		{"10", minTTL},
		{"0", defaultTTL},
		{"-5", defaultTTL},
		{"abc", defaultTTL},
		{"3600", time.Hour},
	}
	for _, c := range cases {
		t.Setenv("STATUSLINE_BILLING_TTL", c.raw)
		if got := effectiveTTL(); got != c.want {
			t.Errorf("TTL(%q) = %v, want %v", c.raw, got, c.want)
		}
		if got := effectiveTTLFor(providerDeepSeek); got != c.want {
			t.Errorf("effectiveTTLFor(deepseek, %q) = %v, want %v", c.raw, got, c.want)
		}
		if got := effectiveTTLFor(providerCodeBuddy); got != c.want {
			t.Errorf("effectiveTTLFor(codebuddy, %q) = %v, want %v", c.raw, got, c.want)
		}
	}
}

func TestFormatAmount(t *testing.T) {
	cases := []struct {
		currency  string
		total     string
		available bool
		want      string
	}{
		{"CNY", "10.26", true, "¥10.26"},
		{"USD", "1.23", true, "$1.23"},
		{"EUR", "5.00", true, "5.00 EUR"},
		{"CNY", "0.00", false, "¥0.00!"},
		{"", "3.50", true, "3.50"},
		{"CNY", "", true, ""},
		{"CNY", "not-a-number", true, ""},
	}
	for _, c := range cases {
		if got := formatAmount(c.currency, c.total, c.available); got != c.want {
			t.Errorf("formatAmount(%q,%q,%v) = %q, want %q", c.currency, c.total, c.available, got, c.want)
		}
	}
}

func TestCachePathIsolatedAndOpaque(t *testing.T) {
	dir := t.TempDir()
	a := agent.BillingSource{Provider: "deepseek", Token: "fixture-token-a"}
	b := agent.BillingSource{Provider: "deepseek", Token: "fixture-token-b"}

	pa, pb := CachePath(dir, a), CachePath(dir, b)
	if pa == pb {
		t.Fatal("different keys must use different cache files")
	}
	if CachePath(dir, a) != pa {
		t.Fatal("same key must be stable")
	}
	if strings.Contains(pa, a.Token) {
		t.Fatalf("cache path leaks the credential: %q", pa)
	}
	if !regexp.MustCompile(`deepseek-[0-9a-f]{16}\.json$`).MatchString(pa) {
		t.Fatalf("unexpected cache name: %q", filepath.Base(pa))
	}
	if filepath.Dir(pa) != filepath.Join(dir, "billing") {
		t.Fatalf("cache dir = %q", filepath.Dir(pa))
	}
}

func TestEntryRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "billing", "deepseek-test.json")
	now := time.Unix(1762473600, 0)
	raw := testResponse("CNY", "10.26", true, `"future_field":{"keep":true}`)
	writeEntry(path, entry{
		lastOK:      now,
		lastAttempt: now,
		status:      statusOK,
		response:    raw,
	})
	got := readEntry(path)
	if got.lastOK.Unix() != now.Unix() || got.lastAttempt.Unix() != now.Unix() {
		t.Errorf("times not preserved: %+v", got)
	}
	if got.status != statusOK {
		t.Errorf("status not preserved: %+v", got)
	}
	if !strings.Contains(string(got.response), "future_field") {
		t.Errorf("unknown response fields were lost: %s", got.response)
	}

	// The on-disk file is JSON, contains the complete response, and never the
	// credential.
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(b) {
		t.Fatalf("cache is not valid JSON: %s", b)
	}
	if !strings.Contains(string(b), "future_field") {
		t.Fatalf("cache lost unknown fields: %s", b)
	}
	if strings.Contains(string(b), "fixture-token") {
		t.Fatalf("cache leaks the credential: %s", b)
	}

	// A failed refresh keeps the last good response and records the attempt.
	writeFailure(path, "http 503")
	got = readEntry(path)
	if got.lastOK.Unix() != now.Unix() || !strings.Contains(string(got.response), "future_field") {
		t.Errorf("failure must preserve the last good response: %+v", got)
	}
	if got.status != statusErr || got.err != "http 503" {
		t.Errorf("failure not recorded: %+v", got)
	}
	if !got.lastAttempt.After(now) {
		t.Errorf("lastAttempt was not advanced: %+v", got.lastAttempt)
	}

	src := agent.BillingSource{Provider: "deepseek"}
	if st := stateFrom(got, src, now, 60*time.Second); st.Amount != "¥10.26" {
		t.Errorf("derived amount = %q, want ¥10.26", st.Amount)
	}
}

func TestEntryRejectsMalformedAndOldFormat(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"not-json":    "not-a-valid-entry\n",
		"old-tsv":     "1762473600\t1762473600\tok\tCNY\t10.26\t10.26\t0.00\t1\t\n",
		"bad-version": `{"version":2,"fetched_at":1,"last_attempt":1,"status":"ok"}`,
	} {
		path := filepath.Join(dir, name+".json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := readEntry(path); !got.lastOK.IsZero() || len(got.response) != 0 {
			t.Fatalf("%s must not yield a balance: %+v", name, got)
		}
	}
}

func TestStateFrom(t *testing.T) {
	src := agent.BillingSource{Provider: "deepseek", Origin: "claude:env:ANTHROPIC_AUTH_TOKEN"}
	now := time.Unix(1762473600, 0)
	ttl := 60 * time.Second

	tests := []struct {
		name       string
		lastOK     time.Time
		status     string
		available  bool
		wantAmount string
		wantStale  bool
	}{
		{"fresh", now.Add(-10 * time.Second), statusOK, true, "¥10.26", false},
		{"stale after 2 TTL", now.Add(-121 * time.Second), statusOK, true, "¥10.26", true},
		{"expired beyond 2h still rendered", now.Add(-3 * time.Hour), statusOK, true, "¥10.26", true},
		{"last attempt failed", now.Add(-10 * time.Second), statusErr, true, "¥10.26", true},
		{"unavailable", now.Add(-10 * time.Second), statusOK, false, "¥10.26!", false},
		{"never fetched", time.Time{}, "", true, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := entry{
				lastOK:   tc.lastOK,
				status:   tc.status,
				response: testResponse("CNY", "10.26", tc.available, ""),
			}
			st := stateFrom(e, src, now, ttl)
			if st.Amount != tc.wantAmount {
				t.Errorf("amount = %q, want %q", st.Amount, tc.wantAmount)
			}
			if st.Stale != tc.wantStale {
				t.Errorf("stale = %v, want %v", st.Stale, tc.wantStale)
			}
			if st.Origin != src.Origin {
				t.Errorf("origin = %q", st.Origin)
			}
			if st.Label != "DeepSeek" {
				t.Errorf("label = %q, want DeepSeek", st.Label)
			}
		})
	}

	bad := entry{lastOK: now, status: statusOK, response: json.RawMessage(`{"balance_infos":[]}`)}
	if st := stateFrom(bad, src, now, ttl); st.Amount != "" || st.Err == "" {
		t.Errorf("invalid response must be reported without an amount: %+v", st)
	}
}

func TestShouldRefresh(t *testing.T) {
	now := time.Unix(1762473600, 0)
	ttl := 60 * time.Second
	cases := []struct {
		name  string
		entry entry
		want  bool
	}{
		{"never attempted", entry{}, true},
		{"ok, within TTL", entry{lastAttempt: now.Add(-10 * time.Second), status: statusOK}, false},
		{"ok, past half TTL but within TTL", entry{lastAttempt: now.Add(-35 * time.Second), status: statusOK}, false},
		{"ok, expired", entry{lastAttempt: now.Add(-61 * time.Second), status: statusOK}, true},
		{"err, within backoff", entry{lastAttempt: now.Add(-30 * time.Second), status: statusErr}, false},
		{"err, backoff expired", entry{lastAttempt: now.Add(-61 * time.Second), status: statusErr}, true},
		{"attempt marked by worker", entry{lastAttempt: now.Add(-10 * time.Second)}, false},
	}
	for _, c := range cases {
		if got := shouldRefresh(c.entry, now, ttl); got != c.want {
			t.Errorf("%s: shouldRefresh = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFetchDeepSeek(t *testing.T) {
	body := `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"10.26","granted_balance":"0.00","topped_up_balance":"10.26"},{"currency":"USD","total_balance":"1.00","granted_balance":"0.00","topped_up_balance":"1.00"}],"future_field":42}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		io.WriteString(w, body)
	}))
	defer srv.Close()

	bal, raw, err := fetchDeepSeek(context.Background(), srv.URL, "fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	if bal.Currency != "CNY" || bal.Total != "10.26" || bal.ToppedUp != "10.26" || bal.Granted != "0.00" || !bal.Available {
		t.Fatalf("unexpected balance: %+v", bal)
	}
	// The cache keeps the complete response: extra currencies and unknown
	// fields must survive even though the bar only renders the first one.
	if !json.Valid(raw) || !strings.Contains(string(raw), "future_field") || !strings.Contains(string(raw), "USD") {
		t.Fatalf("raw response not preserved: %s", raw)
	}
}

func TestFetchDeepSeekErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
		code int
	}{
		{"http error", `{}`, http.StatusInternalServerError},
		{"invalid json", `not-json`, http.StatusOK},
		{"empty infos", `{"is_available":true,"balance_infos":[]}`, http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.code)
				io.WriteString(w, c.body)
			}))
			defer srv.Close()
			if _, _, err := fetchDeepSeek(context.Background(), srv.URL, "fixture-token"); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestFetchDeepSeekRefusesRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("redirect target must not be reached")
	}))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer srv.Close()

	if _, _, err := fetchDeepSeek(context.Background(), srv.URL, "fixture-token"); err == nil {
		t.Fatal("redirect must be treated as an error")
	}
}

func TestLoadDisabled(t *testing.T) {
	t.Setenv("STATUSLINE_TESTING", "1")
	dir := t.TempDir()
	st := Load(dir, "claude")
	if st.Amount != "" || st.Provider != "" {
		t.Fatalf("disabled Load returned state: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(dir, "billing")); !os.IsNotExist(err) {
		t.Fatal("disabled Load must not create the cache directory")
	}
}

func TestLoadWithoutBillableAgent(t *testing.T) {
	t.Setenv("STATUSLINE_TESTING", "")
	t.Setenv("STATUSLINE_BILLING", "")
	if st := Load(t.TempDir(), "generic"); st.Amount != "" {
		t.Fatalf("a generic agent must stay non-billable: %+v", st)
	}
}

func BenchmarkLoadCacheHit(b *testing.B) {
	b.Setenv("STATUSLINE_TESTING", "")
	b.Setenv("STATUSLINE_BILLING", "")
	b.Setenv("STATUSLINE_BILLING_TTL", "60")
	b.Setenv("USERPROFILE", b.TempDir())
	b.Setenv("HOME", b.TempDir())
	b.Setenv("ANTHROPIC_BASE_URL", "https://api.deepseek.com/anthropic")
	b.Setenv("ANTHROPIC_AUTH_TOKEN", "fixture-benchmark-token")
	b.Setenv("ANTHROPIC_API_KEY", "")

	dir := b.TempDir()
	src, ok := pick(SourcesFor("claude"))
	if !ok {
		b.Fatal("no source")
	}
	now := time.Now()
	writeEntry(CachePath(dir, src), entry{
		lastOK: now, lastAttempt: now, status: statusOK,
		response: testResponse("CNY", "10.26", true, ""),
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if st := Load(dir, "claude"); st.Amount != "¥10.26" {
			b.Fatalf("unexpected state: %+v", st)
		}
	}
}
