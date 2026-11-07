package agent

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
)

// CodeBuddy handles the CodeBuddy / WorkBuddy "Status" hook payload.
//
// Differences from the other CLIs:
//   - window usage comes from used_percentage, computed by CodeBuddy itself;
//   - current_usage.input_tokens is only the non-cached delta under the
//     OpenAI-style usage protocol and must not be treated as window usage;
//   - its renderer wraps by byte width, so callers need both a column and a
//     byte budget.
type CodeBuddy struct{}

func (CodeBuddy) Name() string { return "codebuddy" }

func (CodeBuddy) Detect(p *Payload) bool {
	if p.Str("hook_event_name") == "Status" {
		return true
	}
	return strings.Contains(p.Str("transcript_path"), ".codebuddy")
}

func (CodeBuddy) Parse(p *Payload) *Fields {
	f := &Fields{
		Dir:     p.StrAny([]string{"workspace", "current_dir"}, []string{"workspace", "project_dir"}, []string{"cwd"}),
		Model:   p.Str("model", "display_name"),
		Version: p.Str("version"),
		Session: p.StrAny([]string{"session_name"}, []string{"session_id"}),
		Style:   p.Str("output_style", "name"),
	}
	if f.Model == "" {
		f.Model = p.Str("model", "id")
	}
	f.SessionID = p.Str("session_id")
	f.Worktree = p.StrAny([]string{"workspace", "git_worktree"}, []string{"git_worktree"})
	if f.Worktree == "" {
		f.Worktree = p.Str("worktree", "name")
	}

	// current_usage first, cumulative totals as the fallback.
	f.InTokens = p.Int("context_window", "current_usage", "input_tokens")
	f.OutTokens = p.Int("context_window", "current_usage", "output_tokens")
	if f.InTokens == 0 {
		f.InTokens = p.Int("context_window", "total_input_tokens")
	}
	if f.OutTokens == 0 {
		f.OutTokens = p.Int("context_window", "total_output_tokens")
	}
	f.CacheRead = p.Int("context_window", "current_usage", "cache_read_input_tokens")
	if f.CacheRead == 0 {
		f.CacheRead = p.Int("context_window", "cache_read_input_tokens")
	}
	f.CacheCreate = p.Int("context_window", "current_usage", "cache_creation_input_tokens")
	if f.CacheCreate == 0 {
		f.CacheCreate = p.Int("context_window", "cache_creation_input_tokens")
	}
	f.CtxSize = p.Int("context_window", "context_window_size")
	if v, ok := p.Float("context_window", "used_percentage"); ok {
		f.UsedPct = &v
	}

	f.CostUSD, _ = p.Float("cost", "total_cost_usd")
	f.DurMS = p.Int("cost", "total_duration_ms")
	return f
}

// BillingSources exposes the bearer that token-auth sessions carry in the
// environment. The JWT names the account (sub / nickname), so the credits
// snapshot is keyed by uid and labelled with the identity the credential
// itself asserts instead of a possibly stale settings file.
func (CodeBuddy) BillingSources() []BillingSource {
	token := strings.TrimSpace(os.Getenv("CODEBUDDY_AUTH_TOKEN"))
	if token == "" {
		return nil
	}
	uid, account := jwtIdentity(token)
	return []BillingSource{{
		Provider: "codebuddy",
		Token:    token,
		BaseURL:  "https://www.workbuddy.cn",
		UID:      uid,
		Account:  maskAccount(account),
		Origin:   "codebuddy:env:CODEBUDDY_AUTH_TOKEN",
	}}
}

// jwtIdentity reads sub / nickname out of a JWT payload without verifying the
// signature. That is deliberate: the values are display and cache-key material
// only, and the server stays the authority on every request.
func jwtIdentity(token string) (uid, account string) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return "", ""
	}
	var claims struct {
		Sub      string `json:"sub"`
		Nickname string `json:"nickname"`
		Username string `json:"preferred_username"`
		Name     string `json:"name"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return "", ""
	}
	account = claims.Nickname
	if account == "" {
		account = claims.Username
	}
	if account == "" {
		account = claims.Name
	}
	return claims.Sub, account
}

// maskAccount hides a phone number used as a nickname.
func maskAccount(name string) string {
	s := strings.TrimSpace(name)
	if len(s) == 11 && s[0] == '1' && isDigits(s) {
		return s[:3] + "****" + s[7:]
	}
	return s
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
