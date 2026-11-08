package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBarWithSharesTitleCacheForUUIDSession(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("AI_CACHE_HOME", cacheDir)
	t.Setenv("STATUSLINE_TESTING", "1")

	sid := "96efab6b-64f5-4101-a8d4-4683d4b2dd03"
	titleDir := filepath.Join(cacheDir, "statusline", "title")
	if err := os.MkdirAll(titleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cacheFile := filepath.Join(titleDir, "title_"+sid[:8])
	if err := os.WriteFile(cacheFile, []byte("修复状态栏标题与账单\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	payload := []byte(`{
		"product": "antigravity",
		"workspace": {"current_dir": "C:/fixture/project"},
		"model": {"display_name": "Gemini 3.8 Flash"},
		"version": "1.5.3",
		"session_id": "` + sid + `",
		"context_window": {"context_window_size": 1048576, "total_input_tokens": 1000, "total_output_tokens": 200},
		"terminal_width": 140
	}`)

	res := BarWith(payload, "auto", 140, false)
	if !strings.Contains(res.Output, "修复状态栏标题与账单") {
		t.Fatalf("BarWith output does not contain shared title:\n%s", res.Output)
	}
	if strings.Contains(res.Output, sid) {
		t.Fatalf("BarWith output still leaked raw UUID:\n%s", res.Output)
	}
}
