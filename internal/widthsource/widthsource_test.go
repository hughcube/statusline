package widthsource

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveOrder(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "width.live")
	os.WriteFile(live, []byte("150\r\n"), 0o644) // CRLF on purpose

	t.Setenv("STATUSLINE_WIDTH_FILE", live)
	t.Setenv("STATUSLINE_WIDTH", "130")
	if w, src := Resolve(140, ""); w != 150 || src != SourceFile {
		t.Errorf("live file must win: got %d/%s", w, src)
	}

	t.Setenv("STATUSLINE_WIDTH_FILE", filepath.Join(dir, "missing"))
	if w, src := Resolve(140, ""); w != 140 || src != SourceJSON {
		t.Errorf("json must beat env: got %d/%s", w, src)
	}

	if w, src := Resolve(0, ""); w != 130 || src != SourceEnv {
		t.Errorf("env fallback: got %d/%s", w, src)
	}

	if w, src := Resolve(0, "1"); w != 130 || src != SourceEnv {
		t.Errorf("too small override must be ignored: got %d/%s", w, src)
	}

	t.Setenv("STATUSLINE_WIDTH", "")
	if w, src := Resolve(0, ""); w != Default || src != SourceConst {
		t.Errorf("constant fallback: got %d/%s", w, src)
	}
}

func TestResolveExplicitEnvWins(t *testing.T) {
	t.Setenv("STATUSLINE_WIDTH_FILE", "")
	t.Setenv("STATUSLINE_WIDTH", "130")
	if w, src := Resolve(0, "120"); w != 120 || src != SourceEnv {
		t.Errorf("explicit width must override STATUSLINE_WIDTH: got %d/%s", w, src)
	}
}
