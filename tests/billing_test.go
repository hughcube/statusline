package tests

import (
	"strings"
	"testing"
	"time"

	"github.com/hughcube/statusline/internal/agent"
	"github.com/hughcube/statusline/internal/render"
)

func TestBillingSegmentRendering(t *testing.T) {
	setupEnv(t)
	f := &agent.Fields{Model: "deepseek-flash", CtxSize: 1048576, Email: "fixture-user"}

	out := render.BuildWith(f, "claude", 120, render.Git{}, render.Billing{Label: "DeepSeek", Text: "¥10.26"})
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected two lines, got %q", out)
	}
	if !strings.Contains(lines[1], "DeepSeek ¥10.26") {
		t.Fatalf("billing segment missing its label: %q", lines[1])
	}
	acct := strings.Index(lines[1], "fixture-user")
	bal := strings.Index(lines[1], "¥10.26")
	if acct < 0 || bal < 0 || acct > bal {
		t.Fatalf("billing must follow the account: %q", lines[1])
	}

	stale := render.BuildWith(f, "claude", 120, render.Git{}, render.Billing{Label: "DeepSeek", Text: "¥10.26", Stale: true})
	if !strings.Contains(stale, "DeepSeek ¥10.26?") {
		t.Fatalf("stale marker missing: %q", stale)
	}

	// Narrow enough to drop the provider label, still wide enough for the
	// amount: the user's requested degradation order.
	compact := render.BuildWith(f, "claude", 50, render.Git{}, render.Billing{Label: "DeepSeek", Text: "¥10.26"})
	if strings.Contains(compact, "DeepSeek") {
		t.Fatalf("provider label should drop before the amount: %q", compact)
	}
	if !strings.Contains(compact, "¥10.26") {
		t.Fatalf("amount should survive after the label drops: %q", compact)
	}

	narrow := render.BuildWith(f, "claude", 20, render.Git{}, render.Billing{Label: "DeepSeek", Text: "¥10.26"})
	if strings.Contains(narrow, "¥10.26") {
		t.Fatalf("billing must disappear at the last degradation level: %q", narrow)
	}
}

func TestBillingSegmentAbsentWhenEmpty(t *testing.T) {
	setupEnv(t)
	f := &agent.Fields{Model: "deepseek-flash", CtxSize: 1048576}
	out := render.BuildWith(f, "claude", 120, render.Git{}, render.Billing{})
	if strings.Contains(out, "?") || strings.Contains(out, "¥") {
		t.Fatalf("empty billing must not add a segment: %q", out)
	}
}

// The quota segment predates billing and carries its own reset times. Keep
// this covered so the balance work cannot accidentally drop them.
func TestQuotaResetTimesRendering(t *testing.T) {
	setupEnv(t)
	reset5 := time.Unix(1762473600, 0)
	reset7 := time.Unix(1762992000, 0)
	f := &agent.Fields{
		Model:   "Opus",
		CtxSize: 200000,
		Quotas: []agent.Quota{{
			Used5: 23, Used7: 41,
			Reset5: reset5, Reset7: reset7,
		}},
	}
	out := render.BuildWith(f, "claude", 200, render.Git{}, render.Billing{})
	want5 := "23%(" + reset5.Format("15:04") + ")"
	want7 := "41%(" + reset7.Format("02 15:04") + ")"
	if !strings.Contains(out, want5) {
		t.Fatalf("five-hour reset time missing (%q): %q", want5, out)
	}
	if !strings.Contains(out, want7) {
		t.Fatalf("seven-day reset time missing (%q): %q", want7, out)
	}
}
