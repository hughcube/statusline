package mcpcfg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// homeDirFixture points USERPROFILE/HOME at a temp directory and returns it.
func homeDirFixture(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	return home
}

func TestCodeBuddyDirectoryToggle(t *testing.T) {
	home := homeDirFixture(t)
	dir := filepath.Join(home, "work", "demo")
	write(t, filepath.Join(home, ".codebuddy", "mcp.json"), `{
		// two servers, one is turned off for this directory
		"mcpServers": {"alpha": {"command": "a"}, "beta": {"command": "b"}},
		"disabledMcpServers": ["alpha"],
	}`)
	write(t, filepath.Join(home, ".codebuddy.json"), fmt.Sprintf(`{
		"projects": {
			%q: {
				"mcpServers": {},
				"disabledMcpServers": ["beta"],
			},
		},
	}`, filepath.ToSlash(dir)))

	enabled, total := Count("codebuddy", dir)
	if enabled != 1 || total != 2 {
		t.Fatalf("enabled=%d total=%d, want 1/2", enabled, total)
	}
}

func TestCodeBuddyLegacyDisabledIsIgnored(t *testing.T) {
	home := homeDirFixture(t)
	dir := filepath.Join(home, "repo")
	write(t, filepath.Join(home, ".codebuddy", "mcp.json"), `{
		"mcpServers": {"chrome-devtools": {"command": "npx"}},
		"disabledMcpServers": ["chrome-devtools"]
	}`)

	// The CLI never reads disabledMcpServers from the user-scope file, so the
	// server is still enabled; the count must agree with "mcp list".
	enabled, total := Count("codebuddy", dir)
	if enabled != 1 || total != 1 {
		t.Fatalf("enabled=%d total=%d, want 1/1", enabled, total)
	}
}

func TestCodeBuddyProjectScope(t *testing.T) {
	home := homeDirFixture(t)
	dir := filepath.Join(home, "repo")
	write(t, filepath.Join(dir, ".mcp.json"), `{
		"mcpServers": {"local-tool": {"command": "x"}, "other": {"command": "y"}},
		"disabledMcpServers": ["other"]
	}`)

	enabled, total := Count("codebuddy", dir)
	if enabled != 1 || total != 2 {
		t.Fatalf("enabled=%d total=%d, want 1/2", enabled, total)
	}
}

func TestClaudeEntryDisabled(t *testing.T) {
	home := homeDirFixture(t)
	dir := filepath.Join(home, "repo")
	write(t, filepath.Join(home, ".claude.json"), `{
		"mcpServers": {
			"chrome-devtools": {"command": "npx", "disabled": true},
			"other": {"command": "x"}
		}
	}`)

	enabled, total := Count("claude", dir)
	if enabled != 1 || total != 2 {
		t.Fatalf("enabled=%d total=%d, want 1/2", enabled, total)
	}
}

func TestClaudeProjectNeedsApproval(t *testing.T) {
	home := homeDirFixture(t)
	dir := filepath.Join(home, "repo")
	write(t, filepath.Join(dir, ".mcp.json"), `{"mcpServers": {"approved": {}, "pending": {}}}`)
	write(t, filepath.Join(home, ".claude.json"), fmt.Sprintf(`{
		"projects": {%q: {"enabledMcpjsonServers": ["approved"]}}
	}`, filepath.ToSlash(dir)))

	enabled, total := Count("claude", dir)
	if enabled != 1 || total != 2 {
		t.Fatalf("enabled=%d total=%d, want 1/2", enabled, total)
	}
}

func TestAntigravity(t *testing.T) {
	home := homeDirFixture(t)
	write(t, filepath.Join(home, ".gemini", "config", "mcp_config.json"),
		`{"mcpServers": {"chrome-devtools": {"command": "npx"}}}`)

	enabled, total := Count("antigravity", filepath.Join(home, "repo"))
	if enabled != 1 || total != 1 {
		t.Fatalf("enabled=%d total=%d, want 1/1", enabled, total)
	}
}

func TestUnknownAgent(t *testing.T) {
	if e, n := Count("generic", "/tmp"); e != 0 || n != 0 {
		t.Fatalf("got %d/%d, want 0/0", e, n)
	}
}

func TestStripJSONC(t *testing.T) {
	in := `{
		// line comment
		"url": "https://example.com/mcp", /* block */
		"servers": ["a", "b",],
	}`
	var doc map[string]any
	if err := json.Unmarshal(stripJSONC([]byte(in)), &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if doc["url"] != "https://example.com/mcp" {
		t.Fatalf("url = %v", doc["url"])
	}
}

// resetMemo simulates a fresh process: the in-memory memo never survives a
// status-bar frame, so only the disk record can make a repeat frame cheap.
func resetMemo() {
	mu.Lock()
	cache = map[string]result{}
	mu.Unlock()
}

// TestCountCachedReusesTheRecord pins the contract the status bar relies on: a
// repeat frame answers from the record without touching the configuration, and
// an expired record re-reads it.
func TestCountCachedReusesTheRecord(t *testing.T) {
	home := homeDirFixture(t)
	dir := filepath.Join(home, "proj")
	cfg := filepath.Join(home, ".codebuddy", "mcp.json")
	write(t, cfg, `{"mcpServers": {"alpha": {"command": "a"}}}`)
	cacheDir := t.TempDir()

	if enabled, total := CountCached(cacheDir, "codebuddy", dir); enabled != 1 || total != 1 {
		t.Fatalf("enabled=%d total=%d, want 1/1", enabled, total)
	}

	// Delete the config: only the record can answer now.
	if err := os.Remove(cfg); err != nil {
		t.Fatal(err)
	}
	resetMemo()
	if enabled, total := CountCached(cacheDir, "codebuddy", dir); enabled != 1 || total != 1 {
		t.Fatalf("record not reused: enabled=%d total=%d, want 1/1", enabled, total)
	}

	// Expire the record: the config is gone, so the count must drop to zero.
	path := recordPath(cacheDir, "codebuddy", dir)
	r, ok := readRecord(path)
	if !ok {
		t.Fatal("record missing")
	}
	r.At = time.Now().Add(-2 * diskTTL).UnixMilli()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	resetMemo()
	if enabled, total := CountCached(cacheDir, "codebuddy", dir); enabled != 0 || total != 0 {
		t.Fatalf("expired record must re-read: enabled=%d total=%d, want 0/0", enabled, total)
	}
}

// BenchmarkCountCachedRecordHit is the per-frame cost with --mcp: one small file
// read instead of five stats plus the JSONC parses.
func BenchmarkCountCachedRecordHit(b *testing.B) {
	home := b.TempDir()
	b.Setenv("USERPROFILE", home)
	b.Setenv("HOME", home)
	dir := filepath.Join(home, "proj")
	if err := os.MkdirAll(filepath.Join(home, ".codebuddy"), 0o755); err != nil {
		b.Fatal(err)
	}
	cfg := filepath.Join(home, ".codebuddy", "mcp.json")
	if err := os.WriteFile(cfg, []byte(`{"mcpServers": {"a": {"command": "x"}}}`), 0o600); err != nil {
		b.Fatal(err)
	}
	cacheDir := b.TempDir()
	if enabled, _ := CountCached(cacheDir, "codebuddy", dir); enabled != 1 {
		b.Fatal("warm-up failed")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetMemo()
		CountCached(cacheDir, "codebuddy", dir)
	}
}

// BenchmarkCountCold is the path the status bar used before the record existed.
func BenchmarkCountCold(b *testing.B) {
	home := b.TempDir()
	b.Setenv("USERPROFILE", home)
	b.Setenv("HOME", home)
	dir := filepath.Join(home, "proj")
	if err := os.MkdirAll(filepath.Join(home, ".codebuddy"), 0o755); err != nil {
		b.Fatal(err)
	}
	cfg := filepath.Join(home, ".codebuddy", "mcp.json")
	if err := os.WriteFile(cfg, []byte(`{"mcpServers": {"a": {"command": "x"}}}`), 0o600); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resetMemo()
		Count("codebuddy", dir)
	}
}
