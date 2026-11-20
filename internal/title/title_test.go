package title

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hughcube/statusline/internal/agent"
)

func TestGlyphStaticStates(t *testing.T) {
	cases := []struct {
		state string
		isErr bool
		want  string
	}{
		{"idle", false, "❇️"},
		{"idle", true, "❌"},
		{"", false, "❇️"},
		{"error", false, "❌"},
		{"failed", false, "❌"},
		{"cancelled", false, "⏹️"},
		{"waiting_approval", false, "⚠️"},
		{"blocked", false, "⚠️"},
		{"something-new", false, "[something-new]"},
	}
	for _, c := range cases {
		if got := glyph(c.state, c.isErr); got != c.want {
			t.Errorf("glyph(%q,%v) = %q, want %q", c.state, c.isErr, got, c.want)
		}
	}
}

func TestGlyphAnimatedFrames(t *testing.T) {
	working := "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
	thinking := "✦✧✨"
	for i := 0; i < 50; i++ {
		if g := glyph("working", false); !strings.Contains(working, g) {
			t.Fatalf("working glyph %q not a braille frame", g)
		}
		if g := glyph("thinking", false); !strings.Contains(thinking, g) {
			t.Fatalf("thinking glyph %q not a sparkle frame", g)
		}
	}
}

func TestRenderWithSessionName(t *testing.T) {
	f := &agent.Fields{Session: "hello world", Dir: "C:/fixture/project", State: "idle"}
	if got, want := Render(f), "❇️ hello world [project]"; got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

func TestRenderFallsBackToProject(t *testing.T) {
	f := &agent.Fields{Dir: "C:/fixture/project", State: "idle"}
	if got, want := Render(f), "❇️ [project]"; got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

func TestScanTranscriptStreamsFirstLines(t *testing.T) {
	dir := t.TempDir()

	// hit on the third line
	hit := filepath.Join(dir, "hit.jsonl")
	os.WriteFile(hit, []byte(
		"{\"type\":\"MODEL\"}\n"+
			"{\"type\":\"MODEL\"}\n"+
			"{\"type\":\"USER_INPUT\",\"text\":\"<USER_REQUEST>do the thing</USER_REQUEST>\"}\n"), 0o644)
	if got := scanTranscript(hit); got != "do the thing" {
		t.Errorf("scanTranscript = %q, want %q", got, "do the thing")
	}

	// a match beyond the 30-line window must be ignored
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("{\"type\":\"MODEL\"}\n")
	}
	b.WriteString("{\"type\":\"USER_INPUT\",\"text\":\"<USER_REQUEST>too late</USER_REQUEST>\"}\n")
	late := filepath.Join(dir, "late.jsonl")
	os.WriteFile(late, []byte(b.String()), 0o644)
	if got := scanTranscript(late); got != "" {
		t.Errorf("scanTranscript read past the window: %q", got)
	}

	// a very large file must not be read whole: only the head matters
	big := filepath.Join(dir, "big.jsonl")
	f, _ := os.Create(big)
	f.WriteString("{\"type\":\"MODEL\"}\n")
	pad := strings.Repeat("x", 1<<20)
	for i := 0; i < 20; i++ {
		f.WriteString("{\"type\":\"MODEL\",\"pad\":\"" + pad + "\"}\n")
	}
	f.Close()
	if got := scanTranscript(big); got != "" {
		t.Errorf("unexpected title from filler lines: %q", got)
	}
}

func TestCacheDirUsesHomeDir(t *testing.T) {
	t.Setenv("AI_CACHE_HOME", "")
	t.Setenv("USERPROFILE", "C:/fixture/home")
	t.Setenv("HOME", "C:/fixture/home")
	got := filepath.ToSlash(CacheDir())
	if want := "C:/fixture/home/.cache/statusline/title"; got != want {
		t.Errorf("CacheDir = %q, want %q", got, want)
	}
}

func TestRenderIgnoresUUIDAndExtractsTranscript(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("AI_CACHE_HOME", cacheDir)

	sid := "96efab6b-64f5-4101-a8d4-4683d4b2dd03"
	transcriptDir := t.TempDir()
	transcriptPath := filepath.Join(transcriptDir, "transcript.jsonl")
	os.WriteFile(transcriptPath, []byte(
		"{\"type\":\"USER_INPUT\",\"content\":\"<USER_REQUEST>\\n修复状态栏标题与账单\\n</USER_REQUEST>\"}\n",
	), 0o644)

	f := &agent.Fields{
		Session:        sid,
		SessionID:      sid,
		Dir:            "C:/fixture/project",
		State:          "idle",
		TranscriptPath: transcriptPath,
	}

	got := Render(f)
	want := "❇️ 修复状态栏标题与账单 [project]"
	if got != want {
		t.Fatalf("Render with UUID session = %q, want %q", got, want)
	}

	// Next render should hit the unified title cache directly even without transcript
	f.TranscriptPath = filepath.Join(transcriptDir, "non-existent.jsonl")
	gotCached := Render(f)
	if gotCached != want {
		t.Fatalf("Render cached = %q, want %q", gotCached, want)
	}
}

func TestRenderUUIDWithoutTranscriptFallsBackToProject(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("AI_CACHE_HOME", cacheDir)

	sid := "96efab6b-64f5-4101-a8d4-4683d4b2dd03"
	f := &agent.Fields{
		Session:        sid,
		SessionID:      sid,
		Dir:            "C:/fixture/project",
		State:          "idle",
		TranscriptPath: filepath.Join(t.TempDir(), "empty.jsonl"),
	}

	got := Render(f)
	want := "❇️ [project]"
	if got != want {
		t.Fatalf("Render fallback = %q, want %q", got, want)
	}
}

func TestIsSessionIDAndUUID(t *testing.T) {
	cases := []struct {
		s    string
		sid  string
		want bool
	}{
		{"", "123", true},
		{"a6b09b9d-b1d8-4662-b6ca-453e71c1b4d5", "", true},
		{"a6b09b9d-b1d8-4662-b6ca-453e71c1b4d5", "a6b09b9d-b1d8-4662-b6ca-453e71c1b4d5", true},
		{"0123456789abcdef0123456789abcdef", "", true},
		{"a6b09b9d", "a6b09b9d-b1d8-4662-b6ca-453e71c1b4d5", true},
		{"My Session Title", "a6b09b9d-b1d8-4662-b6ca-453e71c1b4d5", false},
		{"项目排查", "", false},
	}
	for _, c := range cases {
		if got := IsSessionID(c.s, c.sid); got != c.want {
			t.Errorf("IsSessionID(%q, %q) = %v, want %v", c.s, c.sid, got, c.want)
		}
	}
}

func BenchmarkRender(b *testing.B) {
	f := &agent.Fields{
		Session:   "Fixed Title",
		Dir:       "C:/fixture/project",
		State:     "idle",
		SessionID: "12345678",
	}
	for i := 0; i < b.N; i++ {
		Render(f)
	}
}

func TestTailState(t *testing.T) {
	dir := t.TempDir()

	// 1. Terminal response: DONE, no tool calls -> idle
	p1 := filepath.Join(dir, "done_no_tools.jsonl")
	os.WriteFile(p1, []byte(
		"{\"type\":\"USER_INPUT\",\"content\":\"hello\"}\n"+
			"{\"type\":\"PLANNER_RESPONSE\",\"status\":\"DONE\",\"tool_calls\":[]}\n",
	), 0o644)
	if got := TailState(p1); got != "idle" {
		t.Errorf("TailState(done_no_tools) = %q, want \"idle\"", got)
	}

	// 2. Active tools: DONE, with tool calls -> "" (still working)
	p2 := filepath.Join(dir, "done_with_tools.jsonl")
	os.WriteFile(p2, []byte(
		"{\"type\":\"USER_INPUT\",\"content\":\"run command\"}\n"+
			"{\"type\":\"PLANNER_RESPONSE\",\"status\":\"DONE\",\"tool_calls\":[{\"name\":\"run\"}]}\n",
	), 0o644)
	if got := TailState(p2); got != "" {
		t.Errorf("TailState(done_with_tools) = %q, want \"\"", got)
	}

	// 3. Terminal error: ERROR -> error
	p3 := filepath.Join(dir, "error.jsonl")
	os.WriteFile(p3, []byte(
		"{\"type\":\"USER_INPUT\",\"content\":\"fail\"}\n"+
			"{\"type\":\"PLANNER_RESPONSE\",\"status\":\"ERROR\"}\n",
	), 0o644)
	if got := TailState(p3); got != "error" {
		t.Errorf("TailState(error) = %q, want \"error\"", got)
	}

	// 4. User input just entered -> ""
	p4 := filepath.Join(dir, "user_input.jsonl")
	os.WriteFile(p4, []byte(
		"{\"type\":\"PLANNER_RESPONSE\",\"status\":\"DONE\",\"tool_calls\":[]}\n"+
			"{\"type\":\"USER_INPUT\",\"content\":\"next question\"}\n",
	), 0o644)
	if got := TailState(p4); got != "" {
		t.Errorf("TailState(user_input) = %q, want \"\"", got)
	}

	// 5. Empty or non-existent file -> ""
	if got := TailState(""); got != "" {
		t.Errorf("TailState(\"\") = %q, want \"\"", got)
	}
	p5 := filepath.Join(dir, "empty.jsonl")
	os.WriteFile(p5, []byte(""), 0o644)
	if got := TailState(p5); got != "" {
		t.Errorf("TailState(empty) = %q, want \"\"", got)
	}

	// 6. Large file (>8KB) with boundary padding
	p6 := filepath.Join(dir, "large.jsonl")
	padding := strings.Repeat("{\"type\":\"GENERIC\",\"content\":\"filler line long enough to pad chunk buffer\"}\n", 200)
	finalLine := "{\"type\":\"PLANNER_RESPONSE\",\"status\":\"DONE\"}\n"
	os.WriteFile(p6, []byte(padding+finalLine), 0o644)
	if got := TailState(p6); got != "idle" {
		t.Errorf("TailState(large) = %q, want \"idle\"", got)
	}
}

func TestRenderSelfHealsWorkingToIdle(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("AI_CACHE_HOME", cacheDir)

	transcriptDir := t.TempDir()
	p := filepath.Join(transcriptDir, "transcript.jsonl")
	os.WriteFile(p, []byte(
		"{\"type\":\"USER_INPUT\",\"content\":\"<USER_REQUEST>\\n测试转录本自愈\\n</USER_REQUEST>\"}\n"+
			"{\"type\":\"PLANNER_RESPONSE\",\"status\":\"DONE\",\"tool_calls\":[]}\n",
	), 0o644)

	f := &agent.Fields{
		Session:        "测试转录本自愈",
		Dir:            "C:/fixture/project",
		State:          "working", // Host payload says "working"
		TranscriptPath: p,
	}

	// Because the transcript tail is DONE with no tool calls, it should self-heal to ❇️ (idle)
	got := Render(f)
	want := "❇️ 测试转录本自愈 [project]"
	if got != want {
		t.Fatalf("Render self-healing got %q, want %q", got, want)
	}

	// If tools are pending, it should remain in working state
	os.WriteFile(p, []byte(
		"{\"type\":\"USER_INPUT\",\"content\":\"<USER_REQUEST>\\n测试转录本自愈\\n</USER_REQUEST>\"}\n"+
			"{\"type\":\"PLANNER_RESPONSE\",\"status\":\"DONE\",\"tool_calls\":[{\"name\":\"bash\"}]}\n",
	), 0o644)
	gotWorking := Render(f)
	if strings.HasPrefix(gotWorking, "❇️") {
		t.Fatalf("Render with pending tools should NOT be idle, got %q", gotWorking)
	}
}
