package tests

import (
	"strings"
	"testing"

	"github.com/hughcube/statusline/internal/agent"
	"github.com/hughcube/statusline/internal/render"
)

func TestCreditsSegmentReplacesAccountSegment(t *testing.T) {
	setupEnv(t)
	f := &agent.Fields{Model: "deepseek-v4.1-flash", Version: "2.181.0"}
	out := render.BuildWith(f, "codebuddy", 120, render.Git{}, render.Billing{
		Account:  "懂",
		Text:     "3,359",
		Expiring: "59",
	})
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected two lines, got %q", out)
	}
	line := lines[1]
	if !strings.Contains(line, "懂 (3,359 | 59 in 24h)") {
		t.Fatalf("credits group missing: %q", line)
	}
	if strings.Count(line, "懂") != 1 {
		t.Fatalf("account must not be printed twice: %q", line)
	}
	if strings.Index(line, "3,359") > strings.Index(line, "deepseek") {
		t.Fatalf("credits must precede the model: %q", line)
	}
}

func TestCreditsSegmentSurvivesWithoutExpiring(t *testing.T) {
	setupEnv(t)
	f := &agent.Fields{Model: "m"}
	out := render.BuildWith(f, "codebuddy", 120, render.Git{}, render.Billing{
		Account: "懂",
		Text:    "3,359",
	})
	if !strings.Contains(out, "懂 (3,359)") {
		t.Fatalf("parenthetical form missing: %q", out)
	}
}
