//go:build diag

package diag

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShardSeparatesAgentsAndSessions(t *testing.T) {
	a := Shard(Info{Agent: "codebuddy", Session: "aaaaaaaa-1111"})
	b := Shard(Info{Agent: "antigravity", Session: "aaaaaaaa-1111"})
	c := Shard(Info{Agent: "codebuddy", Session: "bbbbbbbb-2222"})
	if a == b || a == c || b == c {
		t.Fatalf("shards collide: %q %q %q", a, b, c)
	}
	if !strings.HasPrefix(a, "codebuddy-") {
		t.Errorf("unexpected shard name: %q", a)
	}
}

func TestCaptureWritesPerShard(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AI_CACHE_HOME", root)

	Capture(Info{Kind: "bar", Agent: "codebuddy", Session: "sess-0001", Width: 134,
		WidthSrc: "live", Payload: []byte(`{"a":1}`), Output: "one"})
	Capture(Info{Kind: "bar", Agent: "antigravity", Session: "sess-0002", Width: 120,
		WidthSrc: "json", Payload: []byte(`{"a":2}`), Output: "two"})

	base := filepath.Join(root, "statusline", DirName)
	for _, shard := range []string{"codebuddy-sess-000", "antigravity-sess-0"} {
		_ = shard
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) != 2 {
		t.Fatalf("expected two shard dirs, got %v", dirs)
	}

	b, err := os.ReadFile(filepath.Join(base, "latest.json"))
	if err != nil {
		t.Fatalf("global latest.json missing: %v", err)
	}
	var rec map[string]any
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatalf("global latest.json invalid: %v", err)
	}
	if rec["output"] != "two" {
		t.Errorf("global latest should be the last frame, got %v", rec["output"])
	}
}
