package agent

import "testing"

func TestIsOfficialDeepSeekURL(t *testing.T) {
	official := []string{
		"https://api.deepseek.com",
		"https://api.deepseek.com/",
		"https://api.deepseek.com/anthropic",
		"https://API.DeepSeek.com/anthropic",
		"https://api.deepseek.com:443/anthropic",
	}
	for _, raw := range official {
		if !IsOfficialDeepSeekURL(raw) {
			t.Errorf("%q should be official", raw)
		}
	}

	rejected := []string{
		"",
		"http://api.deepseek.com",
		"https://api.deepseek.com.evil.example",
		"https://evil-api.deepseek.com",
		"https://api.deepseek.com.attacker.test/",
		"https://user@api.deepseek.com",
		"https://api.deepseek.com:8443",
		"https://relay.example/?u=https://api.deepseek.com",
		"ftp://api.deepseek.com",
	}
	for _, raw := range rejected {
		if IsOfficialDeepSeekURL(raw) {
			t.Errorf("%q must not be official", raw)
		}
	}
}

func TestClaudeBillingSourcesFromEnv(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANTHROPIC_BASE_URL", "https://api.deepseek.com/anthropic")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "fixture-token")
	t.Setenv("ANTHROPIC_API_KEY", "fixture-api-key")

	srcs := (Claude{}).BillingSources()
	if len(srcs) != 1 {
		t.Fatalf("got %d sources, want 1", len(srcs))
	}
	if srcs[0].Provider != "deepseek" || srcs[0].Token != "fixture-token" {
		t.Fatalf("unexpected source: %+v", srcs[0])
	}
	if srcs[0].BaseURL != deepSeekBaseURL {
		t.Errorf("base URL = %q, want %q", srcs[0].BaseURL, deepSeekBaseURL)
	}
	if srcs[0].Origin != "claude:env:ANTHROPIC_AUTH_TOKEN" {
		t.Errorf("origin = %q", srcs[0].Origin)
	}
}

func TestClaudeBillingSourcesRejectsRelay(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANTHROPIC_BASE_URL", "https://relay.example/anthropic")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "relay-token")
	t.Setenv("ANTHROPIC_API_KEY", "")

	if srcs := (Claude{}).BillingSources(); len(srcs) != 0 {
		t.Fatalf("relay token must not become a DeepSeek source: %+v", srcs)
	}
}

func TestClaudeBillingSourcesMissingToken(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANTHROPIC_BASE_URL", "https://api.deepseek.com")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	if srcs := (Claude{}).BillingSources(); len(srcs) != 0 {
		t.Fatalf("expected no source without a token: %+v", srcs)
	}
}

func TestOtherAgentsAreNotBillableYet(t *testing.T) {
	for _, a := range []Agent{CodeBuddy{}, Antigravity{}, Generic{}} {
		if _, ok := a.(Billable); ok {
			t.Errorf("%s must not implement Billable yet", a.Name())
		}
	}
}
