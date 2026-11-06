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
	t.Setenv("USERPROFILE", "C:/fixture/home")
	t.Setenv("HOME", "C:/fixture/home")
	got := filepath.ToSlash(CacheDir())
	if want := "C:/fixture/home/.gemini/.statusline-cache"; got != want {
		t.Errorf("CacheDir = %q, want %q", got, want)
	}
}
