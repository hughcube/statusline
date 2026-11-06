package agent

import (
	"net/url"
	"os"
	"strings"
)

// BillingSource is one credential + endpoint pair an agent can offer to the
// billing layer. Only official endpoints are ever returned.
type BillingSource struct {
	Provider string
	Token    string
	BaseURL  string
	// Origin says where the credential came from, for --debug and diag. It
	// never contains the credential itself.
	Origin string
}

// Billable is an optional adapter capability: a CLI that can discover a
// metered API credential from its own environment or settings.
//
// Discovery is deliberately separated from rendering: the billing package
// performs all network and cache work, adapters only answer "is this DeepSeek,
// and which key is in use".
type Billable interface {
	BillingSources() []BillingSource
}

const (
	deepSeekHost    = "api.deepseek.com"
	deepSeekBaseURL = "https://api.deepseek.com"
)

// IsOfficialDeepSeekURL reports whether raw is an HTTPS URL on the official
// DeepSeek API host. A third-party relay whose host merely contains
// "deepseek.com" must never receive the account's official key, so the match is
// exact on the hostname (optionally port 443) and rejects userinfo.
func IsOfficialDeepSeekURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil {
		return false
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return false
	}
	if !strings.EqualFold(u.Hostname(), deepSeekHost) {
		return false
	}
	if p := u.Port(); p != "" && p != "443" {
		return false
	}
	return true
}

// deepSeekFromAnthropicEnv reads the Anthropic-compatible environment that the
// launcher chain injects. The base URL must be the official host; otherwise
// the token belongs to some other provider and is left alone.
func deepSeekFromAnthropicEnv(prefix string) []BillingSource {
	if !IsOfficialDeepSeekURL(os.Getenv("ANTHROPIC_BASE_URL")) {
		return nil
	}
	token, name := firstEnv("ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY")
	if token == "" {
		return nil
	}
	return []BillingSource{{
		Provider: "deepseek",
		Token:    token,
		BaseURL:  deepSeekBaseURL,
		Origin:   prefix + ":env:" + name,
	}}
}

// firstEnv returns the first non-empty, trimmed environment value.
func firstEnv(names ...string) (value, name string) {
	for _, n := range names {
		if v := strings.TrimSpace(os.Getenv(n)); v != "" {
			return v, n
		}
	}
	return "", ""
}
