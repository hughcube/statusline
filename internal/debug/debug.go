// Package debug implements the capture channel used when a status line has to
// be debugged in production.
//
// The hook runs in a short-lived subprocess whose stderr is discarded by the
// host, so the only way to see what actually happened is to write artifacts to
// disk. When STATUSLINE_DEBUG_DIR is set, every invocation records:
//
//	latest.json      - one atomically replaced file: meta + payload + output
//	frames/NNNN.json - a bounded ring of the same records for comparison
//
// Everything is best effort: a failure here must never break the status line.
package debug

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Record is one captured invocation.
type Record struct {
	Time     string          `json:"time"`
	Kind     string          `json:"kind"` // bar | title
	Agent    string          `json:"agent"`
	Width    int             `json:"width"`
	WidthSrc string          `json:"width_src"`
	Duration int64           `json:"duration_ms"`
	Payload  json.RawMessage `json:"payload"`
	Output   string          `json:"output"`
	Extra    any             `json:"extra,omitempty"`
}

// ringSize is how many frames are kept under frames/.
const ringSize = 100

var (
	mu   sync.Mutex
	seq  int
	seen bool
)

// Dir returns the capture directory when the channel is enabled.
func Dir() (string, bool) {
	d := strings.TrimSpace(os.Getenv("STATUSLINE_DEBUG_DIR"))
	if d == "" {
		return "", false
	}
	if err := os.MkdirAll(filepath.Join(d, "frames"), 0o755); err != nil {
		return "", false
	}
	return d, true
}

// Capture records one invocation. payload must be valid JSON; if it is not, it
// is stored as a JSON string so the file stays parseable.
func Capture(dir, kind, agent string, width int, widthSrc string, dur time.Duration, payload []byte, output string, extra any) {
	if dir == "" {
		return
	}
	rec := Record{
		Time:     time.Now().Format(time.RFC3339Nano),
		Kind:     kind,
		Agent:    agent,
		Width:    width,
		WidthSrc: widthSrc,
		Duration: dur.Milliseconds(),
		Payload:  json.RawMessage(payload),
		Output:   output,
		Extra:    extra,
	}
	if !json.Valid(rec.Payload) {
		quoted, _ := json.Marshal(string(payload))
		rec.Payload = quoted
	}
	blob, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Join(dir, "frames"), 0o755); err != nil {
		return
	}
	writeAtomic(filepath.Join(dir, "latest.json"), blob)

	mu.Lock()
	defer mu.Unlock()
	if !seen {
		seen = true
		seq = nextSeq(dir)
	}
	seq++
	name := fmt.Sprintf("%04d-%s.json", seq%10000, kind)
	writeAtomic(filepath.Join(dir, "frames", name), blob)
	prune(filepath.Join(dir, "frames"))
}

func writeAtomic(path string, data []byte) {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func nextSeq(dir string) int {
	entries, err := os.ReadDir(filepath.Join(dir, "frames"))
	if err != nil {
		return 0
	}
	max := 0
	for _, e := range entries {
		name := e.Name()
		if i := strings.IndexByte(name, '-'); i > 0 {
			if n, err := strconv.Atoi(name[:i]); err == nil && n > max {
				max = n
			}
		}
	}
	return max
}

// prune keeps the frames directory bounded; zero-padded names sort by age.
func prune(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= ringSize {
		return
	}
	excess := len(entries) - ringSize
	for i := 0; i < excess; i++ {
		_ = os.Remove(filepath.Join(dir, entries[i].Name()))
	}
}
