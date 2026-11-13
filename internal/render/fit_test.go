package render

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hughcube/statusline/internal/agent"
)

// csiLine1 matches the cursor layout CodeBuddy uses: ESC[1G, left, ESC[nC, right.
var csiLine1 = regexp.MustCompile("\x1b\\[1G(.*)\x1b\\[([0-9]+)C(.*)")

// TestLineOneNeverOverflowsTheContainer guards the bug that made the status
// area grow a row: line 1 is assembled from segments that were never budgeted
// (the git branch, the usage counters, the auto-derived session title), so a
// narrow window or a busy frame could exceed the container while CodeBuddy
// counts bytes and the terminal counts columns.
func TestLineOneNeverOverflowsTheContainer(t *testing.T) {
	plain := &agent.Fields{Dir: "C:/fixture/project", Model: "Opus", Version: "2.0.0", CtxSize: 1000000}
	busy := &agent.Fields{
		Dir:         "C:/Users/someone/Data/a-deeply/nested/project/directory",
		Session:     "修复状态栏在 CodeBuddy 下跑着跑着就不显示的问题并重新发布验证",
		Model:       "deepseek-v4.1-flash 1.0M think",
		Version:     "2.138.0",
		InTokens:    1234567,
		OutTokens:   98765,
		CacheRead:   9876543,
		CacheCreate: 2345678,
		CtxSize:     1000000,
		CostUSD:     12.34,
		DurMS:       10860000,
		MCPEnabled:  7,
		Quotas:      []agent.Quota{{Label: "weekly", Used5: 91, Used7: 43}},
	}
	gits := []Git{
		{},
		{Branch: "feature/a-very-long-branch-name-that-should-yield", Dirty: 128, Ahead: 12, Behind: 3, Add: 4321, Del: 987, Worktree: "wt-name-here"},
	}
	clients := []string{"codebuddy", "antigravity", "claude", "generic"}
	bill := Billing{Label: "DeepSeek", Text: "¥10.26"}

	for _, f := range []*agent.Fields{plain, busy} {
		for _, g := range gits {
			for _, client := range clients {
				for width := 20; width <= 220; width++ {
					st := StyleFor(client, width)
					wt := st.Container
					if wt < 20 {
						wt = 20
					}
					line1 := firstLine(BuildWith(f, client, width, g, bill))
					if got := visualSegWidth(line1); got > wt {
						t.Fatalf("%s w=%d: line 1 is %d columns, container is %d: %q", client, width, got, wt, line1)
					}
					if !st.Cursor {
						continue
					}
					m := csiLine1.FindStringSubmatch(line1)
					if m == nil {
						t.Fatalf("%s w=%d: line 1 is not in the cursor layout: %q", client, width, line1)
					}
					gap, _ := strconv.Atoi(m[2])
					left, right := m[1], m[3]
					if SegWidth(left)+gap+SegWidth(right) > wt {
						t.Fatalf("%s w=%d: left %d + gap %d + right %d exceeds the container %d", client, width, SegWidth(left), gap, SegWidth(right), wt)
					}
					// The byte budget is a budget for content: the padding that
					// pushes the right block flush is not part of it.
					if got := ByteWidth(left) + ByteWidth(right); got > st.ByteLimit {
						t.Fatalf("%s w=%d: line 1 content is %d bytes, budget is %d: %q", client, width, got, st.ByteLimit, line1)
					}
				}
			}
		}
	}
}

// TestLineTwoNeverOverflowsTheContainer is the same guard for line 2, which is
// where a CJK account name costs three bytes for every two columns.
func TestLineTwoNeverOverflowsTheContainer(t *testing.T) {
	f := &agent.Fields{
		Dir:      "C:/fixture/project",
		Model:    "deepseek-v4.1-flash 1.0M think",
		Version:  "2.138.0",
		Email:    "someone@example.com",
		PlanTier: "max",
	}
	bill := Billing{Account: "小懂同学的小号", Text: "3,359", Expiring: "59"}
	for _, client := range []string{"codebuddy", "antigravity", "claude", "generic"} {
		for width := 20; width <= 220; width++ {
			st := StyleFor(client, width)
			wt := st.Container
			if wt < 20 {
				wt = 20
			}
			lines := splitLines(BuildWith(f, client, width, Git{}, bill))
			if len(lines) < 2 {
				t.Fatalf("%s w=%d: render has %d lines, want 2: %q", client, width, len(lines), lines)
			}
			if got := visualSegWidth(lines[1]); got > wt {
				t.Fatalf("%s w=%d: line 2 is %d columns, container is %d: %q", client, width, got, wt, lines[1])
			}
		}
	}
}

// ansiForward matches a cursor-forward escape; ansiSimple matches any other
// CSI sequence these renderers emit.
var (
	ansiForward = regexp.MustCompile("\x1b\\[([0-9]*)C")
	ansiSimple  = regexp.MustCompile("\x1b\\[([0-9]*)[A-Za-z]")
)

// visualSegWidth measures what the terminal draws: cursor moves become the
// spaces they advance over, other escapes disappear. Measuring the raw string
// would count escape bytes as columns.
func visualSegWidth(s string) int {
	s = ansiForward.ReplaceAllStringFunc(s, func(m string) string {
		n := 1
		if v := ansiSimple.FindStringSubmatch(m); v != nil && v[1] != "" {
			n, _ = strconv.Atoi(v[1])
		}
		return strings.Repeat(" ", n)
	})
	s = ansiSimple.ReplaceAllString(s, "")
	return SegWidth(s)
}

// firstLine returns the first physical line of a render.
func firstLine(s string) string {
	if i := indexByte(s, 0x0a); i >= 0 {
		return s[:i]
	}
	return s
}

// splitLines splits a render on its newlines.
func splitLines(s string) []string {
	var out []string
	for {
		i := indexByte(s, 0x0a)
		if i < 0 {
			return append(out, s)
		}
		out = append(out, s[:i])
		s = s[i+1:]
	}
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

var _ = fmt.Sprintf
