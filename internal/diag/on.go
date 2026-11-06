//go:build diag

package diag

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

// Enabled reports whether this build captures diagnostics.
func Enabled() bool { return true }

// Dir returns the capture directory.
func Dir() string {
	root := os.Getenv("AI_CACHE_HOME")
	if root == "" {
		home := os.Getenv("HOME")
		if home == "" {
			home = os.Getenv("USERPROFILE")
		}
		root = filepath.Join(home, ".cache")
	}
	return filepath.Join(root, "statusline", DirName)
}

type record struct {
	Time     string          `json:"time"`
	Kind     string          `json:"kind"`
	Build    build           `json:"build"`
	Agent    string          `json:"agent"`
	Width    int             `json:"width"`
	WidthSrc string          `json:"width_src"`
	Duration int64           `json:"duration_ms"`
	Payload  json.RawMessage `json:"payload"`
	Output   string          `json:"output"`
	Extra    any             `json:"extra,omitempty"`
}

type build struct {
	Version string `json:"version"`
	Exe     string `json:"exe"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}

var (
	mu   sync.Mutex
	seq  int
	seen bool
)

// Capture writes the frame to <cache>/statusline/diag/latest.json plus a small
// ring so several frames can be compared. Best effort by design.
func Capture(in Info) {
	dir := Dir()
	if err := os.MkdirAll(filepath.Join(dir, "frames"), 0o755); err != nil {
		return
	}
	rec := record{
		Time:     time.Now().Format(time.RFC3339Nano),
		Kind:     in.Kind,
		Build:    currentBuild(),
		Agent:    in.Agent,
		Width:    in.Width,
		WidthSrc: in.WidthSrc,
		Duration: in.DurationMS,
		Payload:  json.RawMessage(in.Payload),
		Output:   in.Output,
		Extra:    in.Extra,
	}
	if !json.Valid(rec.Payload) {
		quoted, _ := json.Marshal(string(in.Payload))
		rec.Payload = quoted
	}
	blob, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return
	}
	writeAtomic(filepath.Join(dir, "latest.json"), blob)

	mu.Lock()
	defer mu.Unlock()
	if !seen {
		seen = true
		seq = nextSeq(filepath.Join(dir, "frames"))
	}
	seq++
	writeAtomic(filepath.Join(dir, "frames", fmt.Sprintf("%05d-%s.json", seq%100000, in.Kind)), blob)
	prune(filepath.Join(dir, "frames"), 50)
}

func currentBuild() build {
	b := build{Version: version}
	if exe, err := os.Executable(); err == nil {
		b.Exe = exe
		if st, err := os.Stat(exe); err == nil {
			b.Size = st.Size()
			b.ModTime = st.ModTime().Format(time.RFC3339)
		}
	}
	return b
}

// version is injected by main through SetVersion.
var version = "dev"

// SetVersion records the build version in captured frames.
func SetVersion(v string) { version = v }

func writeAtomic(path string, data []byte) {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func nextSeq(dir string) int {
	entries, err := os.ReadDir(dir)
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

func prune(dir string, max int) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= max {
		return
	}
	for i := 0; i < len(entries)-max; i++ {
		_ = os.Remove(filepath.Join(dir, entries[i].Name()))
	}
}
