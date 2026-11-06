// Package tests holds cross-package contract tests.
//
// Golden files under testdata/golden were produced by the legacy shell
// implementation, so these assertions mean "byte-identical to the historical
// behaviour", not "matches whatever the current code happens to print".
package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hughcube/statusline/internal/app"
	"github.com/hughcube/statusline/internal/render"
)

// renderBar renders one status bar with a fixed environment.
func renderBar(t *testing.T, payload string, width int) string {
	t.Helper()
	setupEnv(t)
	return app.Bar([]byte(payload), "auto", width).Output
}

func assertParts(t *testing.T, out string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(out, p) {
			t.Errorf("missing fragment %q\noutput:\n%s", p, out)
		}
	}
}

func assertNotParts(t *testing.T, out string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if strings.Contains(out, p) {
			t.Errorf("unexpected fragment %q\noutput:\n%s", p, out)
		}
	}
}

func TestContractClaude(t *testing.T) {
	out := renderBar(t, `{"workspace":{"current_dir":"C:/fixture/project"},"model":{"display_name":"Opus"},"version":"2.1.0","context_window":{"total_input_tokens":15500,"total_output_tokens":1200,"context_window_size":200000,"current_usage":{"cache_read_input_tokens":2000,"cache_creation_input_tokens":5000}},"cost":{"total_cost_usd":0.42,"total_duration_ms":3600000},"rate_limits":{"five_hour":{"used_percentage":23.5},"seven_day":{"used_percentage":41.2}},"session_name":"my-session","effort":{"level":"high"}}`, 120)
	assertParts(t, out,
		"16.7K(8%)", "$0.42",
		"In:15.5K Out:1.2K Cache:2.0K CacheWrite:5.0K",
		"23%/41%", "Opus 200.0K high", "v2.1.0", "1h0m")
}

func TestContractAntigravity(t *testing.T) {
	out := renderBar(t, `{"product":"antigravity","workspace":{"current_dir":"C:/fixture/project"},"model":{"display_name":"Gemini 3.7 Flash (High)"},"version":"1.1.17","session_name":"session title","plan_tier":"Google AI Pro","context_window":{"context_window_size":1048576,"total_input_tokens":32800,"total_output_tokens":7300,"cache_read_input_tokens":25000},"quota":{"3p-5h":{"remaining_fraction":0.68},"3p-weekly":{"remaining_fraction":0.85},"gemini-5h":{"remaining_fraction":0.64},"gemini-weekly":{"remaining_fraction":0.9}},"effort":{"level":"High"},"thinking":{"enabled":true},"terminal_width":120}`, 120)
	assertParts(t, out,
		"40.1K(4%)", "In:32.8K Out:7.3K Cache:25.0K", "Pro",
		"P·32%", "G·36%", "Gemini 3.7 Flash 1.0M high think", "v1.1.17")
}

func TestContractSingleQuota(t *testing.T) {
	out := renderBar(t, `{"product":"antigravity","model":{"display_name":"Gemini"},"version":"1","quota":{"remaining_fraction":0.85},"terminal_width":120}`, 120)
	assertParts(t, out, "G·15%")
}

func TestContractMinimalAndZero(t *testing.T) {
	out := renderBar(t, `{"model":{"display_name":"Mystery CLI"},"version":"1.0.0"}`, 100)
	assertParts(t, out, "Mystery CLI", "v1.0.0")

	out = renderBar(t, `{"model":{"display_name":"M"},"version":"1","context_window":{"context_window_size":0},"session_id":"zz"}`, 100)
	assertParts(t, out, "0.0K(0%)", "In:0.0K Out:0.0K", "| zz")
}

func TestContractWorktree(t *testing.T) {
	out := renderBar(t, `{"workspace":{"current_dir":"C:/fixture/project","git_worktree":"feature-xyz"},"model":{"display_name":"Opus"},"version":"2.0.0","session_id":"11111111-2222-3333-4444-555555555555"}`, 120)
	assertParts(t, out, "wt:feature-xyz", "11111111")
	assertNotParts(t, out, "working")
}

func TestContractUserCase(t *testing.T) {
	out := renderBar(t, `{"workspace":{"current_dir":"C:/fixture/project"},"model":{"display_name":"Gemini 3.7 Flash (High)"},"version":"1.1.17","context_window":{"context_window_size":1048576,"total_input_tokens":114600,"total_output_tokens":33300,"cache_read_input_tokens":110000},"product":"antigravity"}`, 160)
	assertParts(t, out, "147.9K(14%) | In:114.6K Out:33.3K Cache:110.0K")
}

func TestContractModelVariants(t *testing.T) {
	cases := []struct{ model, want string }{
		{"Claude Opus 4.6 (Thinking)", "Claude Opus 4.6 250.0K think"},
		{"Claude Sonnet 4.6 (Thinking)", "Claude Sonnet 4.6 250.0K think"},
		{"Gemini 3.7 Flash (High)", "Gemini 3.7 Flash 1.0M high"},
		{"Gemini 3.7 Flash (Low)", "Gemini 3.7 Flash 1.0M low"},
		{"Gemini 3.7 Flash(think)", "Gemini 3.7 Flash 1.0M think"},
		{"Gemini 3.7 Flash (High) (Thinking)", "Gemini 3.7 Flash 1.0M high think"},
		{"GPT-OSS 120B (Medium)", "GPT-OSS 120B 250.0K medium"},
		{"Gemini 3.7 Flash (no-think)", "Gemini 3.7 Flash 1.0M no-think"},
	}
	for _, c := range cases {
		payload := fmt.Sprintf(`{"product":"antigravity","model":{"display_name":%q},"context_window":{"context_window_size":%d},"version":"1","terminal_width":157}`, c.model, ctxFor(c.want))
		out := renderBar(t, payload, 157)
		if !strings.Contains(out, c.want) {
			t.Errorf("model %q: want %q, got:\n%s", c.model, c.want, out)
		}
	}
}

// ctxFor returns the context window that belongs to the expected rendering.
func ctxFor(want string) int {
	if strings.Contains(want, "250.0K") {
		return 250000
	}
	return 1048576
}

func TestContractG4Quota(t *testing.T) {
	out := renderBar(t, `{"product":"antigravity","model":{"display_name":"Gemini 3.8 Flash"},"plan_tier":"Google AI Pro","version":"1.5.2","quota":{"gemini-argon-wtu-weekly":{"remaining_fraction":0}}}`, 180)
	assertParts(t, out, "G4·100%")

	out = renderBar(t, `{"product":"antigravity","model":{"display_name":"Gemini 3.8 Flash"},"plan_tier":"Google AI Pro","version":"1.5.2","quota":{"gemini-argon-5h":{"remaining_fraction":0.8},"gemini-argon-wtu-weekly":{"remaining_fraction":0.5}}}`, 180)
	assertParts(t, out, "G4·20%/50%")
}

func TestContractCodeBuddy(t *testing.T) {
	out := renderBar(t, `{"hook_event_name":"Status","session_id":"92c90ada","session_name":"code review session","workspace":{"current_dir":"C:/fixture/home/proj"},"model":{"display_name":"Deepseek-V4-Flash"},"version":"2.138.0","context_window":{"total_input_tokens":19497108,"total_output_tokens":98896,"context_window_size":1000000,"current_usage":{"input_tokens":357,"output_tokens":275,"cache_read_input_tokens":593664},"used_percentage":59.4}}`, 157)
	assertParts(t, out,
		"594.0K(59%)", "In:0.3K Out:0.2K Cache:593.6K", "~",
		"Deepseek-V4-Flash 1.0M", "v2.138.0", "code review session")
	assertNotParts(t, out, "200k+", "5h ", "7d ")

	// CodeBuddy wraps by byte width, so line 1 must fit the byte budget too.
	line1 := strings.SplitN(out, "\n", 2)[0]
	if bw := render.ByteWidth(stripCSI(line1)); bw > 157 {
		t.Errorf("line 1 byte width = %d, want <= 157", bw)
	}
}

func TestContractModelClean(t *testing.T) {
	out := renderBar(t, `{"hook_event_name":"Status","session_id":"s1","workspace":{"current_dir":"C:/fixture/project"},"model":{"display_name":"deepseek-v4-flash[1m]"},"version":"2.142.0","context_window":{"context_window_size":1000000}}`, 120)
	assertNotParts(t, out, "[1m]")
	assertParts(t, out, "deepseek-v4-flash 1.0M")

	out = renderBar(t, `{"hook_event_name":"Status","session_id":"s1","workspace":{"current_dir":"C:/fixture/project"},"model":{"display_name":"Deepseek-V4-Flash"},"version":"2.142.0","context_window":{"context_window_size":1000000}}`, 120)
	assertParts(t, out, "Deepseek-V4-Flash 1.0M")
}

func TestContractUsedPctFallback(t *testing.T) {
	// used_percentage 为 null 或缺失时,必须回退到 in+out 口径。
	base := `{"hook_event_name":"Status","session_id":"s1","workspace":{"current_dir":"C:/fixture/project"},"model":{"display_name":"Kimi-K3"},"version":"2.140.0","context_window":{"context_window_size":1000000,"current_usage":{"input_tokens":1000,"output_tokens":500}}}`
	withNull := `{"hook_event_name":"Status","session_id":"s1","workspace":{"current_dir":"C:/fixture/project"},"model":{"display_name":"Kimi-K3"},"version":"2.140.0","context_window":{"context_window_size":1000000,"current_usage":{"input_tokens":1000,"output_tokens":500},"used_percentage":null}}`
	for _, payload := range []string{base, withNull} {
		out := renderBar(t, payload, 120)
		assertParts(t, out, "1.5K(0%)")
	}
}

func TestContractANSI(t *testing.T) {
	cb := renderBar(t, `{"hook_event_name":"Status","session_id":"s1","workspace":{"current_dir":"C:/fixture/project"},"model":{"display_name":"M"},"version":"1","context_window":{"context_window_size":1000000}}`, 157)
	lines := strings.SplitN(cb, "\n", 2)
	if !strings.HasPrefix(lines[0], "\x1b[1G") {
		t.Errorf("codebuddy line 1 must start with CSI 1G: %q", lines[0])
	}
	if !strings.Contains(lines[0], "\x1b[") || !strings.Contains(lines[0], "C") {
		t.Errorf("codebuddy line 1 must use CUF positioning")
	}

	cl := renderBar(t, `{"workspace":{"current_dir":"C:/fixture/project"},"model":{"display_name":"Opus"},"version":"2.1.0","context_window":{"context_window_size":200000}}`, 120)
	if strings.Contains(cl, "\x1b[1G") || strings.Contains(cl, "\x1b[97m") {
		t.Errorf("claude must not emit CHA/bright-white: %q", cl)
	}
	if !strings.Contains(cl, "\n\x1b[0m") {
		t.Errorf("claude line 2 must start with ESC0 padding")
	}
}

func TestContractRightAlign(t *testing.T) {
	for _, width := range []int{80, 120, 157} {
		cb := renderBar(t, `{"hook_event_name":"Status","session_id":"s1","workspace":{"current_dir":"C:/fixture/project"},"model":{"display_name":"M"},"version":"1","context_window":{"context_window_size":1000000}}`, width)
		if got := rightEdge(cb); got != width-1 {
			t.Errorf("codebuddy width %d: right edge = %d, want %d", width, got, width-1)
		}
		cl := renderBar(t, `{"workspace":{"current_dir":"C:/fixture/project"},"model":{"display_name":"Opus"},"version":"2.1.0","context_window":{"context_window_size":200000}}`, width)
		if got := rightEdge(cl); got != width-4 {
			t.Errorf("claude width %d: right edge = %d, want %d", width, got, width-4)
		}
	}
}

// rightEdge returns CUF offset plus visible width (codebuddy) or visible width alone.
func rightEdge(out string) int {
	lines := strings.SplitN(out, "\n", 2)
	l2 := lines[len(lines)-1]
	cuf := 0
	if i := strings.LastIndex(l2, "\x1b["); i >= 0 {
		var n int
		if _, err := fmt.Sscanf(l2[i:], "\x1b[%dC", &n); err == nil {
			cuf = n
		}
	}
	return cuf + render.SegWidth(stripCSI(l2))
}

// stripCSI removes ANSI control sequences.
func stripCSI(s string) string {
	var b strings.Builder
	inCSI := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inCSI {
			if c >= '@' && c <= '~' {
				inCSI = false
			}
			continue
		}
		if c == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			inCSI = true
			i++
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func TestContractThinkFromSettings(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codebuddy"), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := `{"hook_event_name":"Status","session_id":"s1","workspace":{"current_dir":"C:/fixture/project"},"model":{"display_name":"Kimi-K3"},"version":"1","context_window":{"context_window_size":1000000}}`

	cases := []struct {
		settings string
		want     []string
		notWant  []string
	}{
		{`{}`, []string{"think"}, nil},
		{`{"reasoningEffort":"xhigh"}`, []string{"xhigh think"}, nil},
		{`{"reasoningEffort":"xhigh","alwaysThinkingEnabled":false}`, []string{"no-think"}, []string{"xhigh"}},
	}
	for _, c := range cases {
		if err := os.WriteFile(filepath.Join(home, ".codebuddy", "settings.json"), []byte(c.settings), 0o644); err != nil {
			t.Fatal(err)
		}
		setupEnv(t)
		t.Setenv("HOME", home)
		out := app.Bar([]byte(payload), "auto", 120).Output
		assertParts(t, out, c.want...)
		if len(c.notWant) > 0 {
			assertNotParts(t, out, c.notWant...)
		}
	}
}
