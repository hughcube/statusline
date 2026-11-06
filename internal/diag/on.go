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

// Dir returns the capture root.
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
	Shard    string          `json:"shard"`
	Build    build           `json:"build"`
	Agent    string          `json:"agent"`
	Session  string          `json:"session,omitempty"`
	Width    int             `json:"width"`
	WidthSrc string          `json:"width_src"`
	Duration int64           `json:"duration_ms"`
	Payload  json.RawMessage `json:"payload"`
	Output   string          `json:"output"`
	Extra    any             `json:"extra,omitempty"`
	Billing  any             `json:"billing,omitempty"`
}

type build struct {
	Version string `json:"version"`
	Exe     string `json:"exe"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}

var (
	mu   sync.Mutex
	seq  = map[string]int{}
	seen = map[string]bool{}
)

// version is injected by main through SetVersion.
var version = "dev"

// SetVersion records the build version in captured frames.
func SetVersion(v string) { version = v }

// Shard returns the per-agent+session directory name. Sessions shorter than
// four characters fall back to the pid so anonymous windows stay apart.
func Shard(in Info) string {
	agent := sanitize(in.Agent)
	if agent == "" {
		agent = "unknown"
	}
	s := sanitize(strings.TrimSpace(in.Session))
	if len(s) >= 4 {
		if len(s) > 8 {
			s = s[:8]
		}
		return agent + "-" + s
	}
	return agent + "-pid" + strconv.Itoa(os.Getpid())
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
}

// Capture writes the frame under its shard plus a global latest pointer.
func Capture(in Info) {
	shard := Shard(in)
	dir := filepath.Join(Dir(), shard)
	if err := os.MkdirAll(filepath.Join(dir, "frames"), 0o755); err != nil {
		return
	}

	rec := record{
		Time:     time.Now().Format(time.RFC3339Nano),
		Kind:     in.Kind,
		Shard:    shard,
		Build:    currentBuild(),
		Agent:    in.Agent,
		Session:  in.Session,
		Width:    in.Width,
		WidthSrc: in.WidthSrc,
		Duration: in.DurationMS,
		Payload:  json.RawMessage(in.Payload),
		Output:   in.Output,
		Extra:    in.Extra,
		Billing:  in.Billing,
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
	writeAtomic(filepath.Join(Dir(), "latest.json"), blob)

	mu.Lock()
	defer mu.Unlock()
	if !seen[shard] {
		seen[shard] = true
		seq[shard] = nextSeq(filepath.Join(dir, "frames"))
	}
	seq[shard]++
	name := fmt.Sprintf("%05d-%s.json", seq[shard]%100000, in.Kind)
	writeAtomic(filepath.Join(dir, "frames", name), blob)
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
