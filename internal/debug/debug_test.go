package debug

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCaptureWritesLatestAndRing(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		Capture(dir, "bar", "codebuddy", 157, "env", 5*time.Millisecond,
			[]byte(`{"model":{"display_name":"Demo"}}`), "line1\nline2", nil)
	}

	b, err := os.ReadFile(filepath.Join(dir, "latest.json"))
	if err != nil {
		t.Fatalf("latest.json missing: %v", err)
	}
	var rec Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatalf("latest.json is not valid JSON: %v", err)
	}
	if rec.Kind != "bar" || rec.Agent != "codebuddy" || rec.Width != 157 {
		t.Errorf("unexpected record: %+v", rec)
	}
	if rec.Output != "line1\nline2" {
		t.Errorf("output not captured: %q", rec.Output)
	}

	frames, err := os.ReadDir(filepath.Join(dir, "frames"))
	if err != nil {
		t.Fatalf("frames dir missing: %v", err)
	}
	if len(frames) != 3 {
		t.Errorf("frames = %d, want 3", len(frames))
	}
}

func TestCaptureHandlesInvalidPayload(t *testing.T) {
	dir := t.TempDir()
	Capture(dir, "bar", "generic", 120, "const", time.Millisecond, []byte("not json"), "x", nil)
	b, err := os.ReadFile(filepath.Join(dir, "latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec Record
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatalf("capture must stay parseable: %v", err)
	}
}
