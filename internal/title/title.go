// Package title produces the terminal window title used by Antigravity and
// other CLIs.
//
// The status glyph comes from the agent_state field supplied by the host; this
// package never guesses. The session title is resolved in this order:
// session_name -> local cache -> first <USER_REQUEST> found in the transcript.
package title

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hughcube/statusline/internal/agent"
	"github.com/hughcube/statusline/internal/render"
)

// MaxTitleCols is the column budget for the session part of the title.
const MaxTitleCols = 28

// negativeTTL throttles transcript scans so high-frequency rendering cannot
// hammer the disk.
const negativeTTL = 5 * time.Second

// CacheDir returns the title cache directory, kept identical to the legacy
// implementation so upgrades are seamless.
func CacheDir() string {
	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}
	return filepath.Join(home, ".gemini", ".statusline-cache")
}

// Render builds the title text.
func Render(f *agent.Fields) string {
	project := render.BaseName(f.Dir)
	if project == "" {
		project = "workspace"
	}

	sessionTitle := ""
	if s := clean(f.Session); s != "" {
		sessionTitle = render.TruncHead(s, MaxTitleCols)
	}
	if sessionTitle == "" {
		sessionTitle = fromCache(f.SessionID)
	}
	if sessionTitle == "" {
		sessionTitle = fromTranscript(f)
	}

	status := glyph(f.State, f.IsError)
	if sessionTitle != "" {
		return fmt.Sprintf("%s %s [%s]", status, sessionTitle, project)
	}
	return fmt.Sprintf("%s [%s]", status, project)
}

// glyph maps agent_state to an icon. working/thinking animate by the clock.
func glyph(state string, isError bool) string {
	switch state {
	case "working":
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		return frames[int((millis()/73)%int64(len(frames)))]
	case "thinking":
		frames := []string{"✦", "✧", "✦", "✧", "✨", "✦", "✧", "✦"}
		return frames[int((millis()/113)%int64(len(frames)))]
	case "error", "failed", "failure", "exception":
		return "❌"
	case "cancelled", "canceled", "interrupted", "stopped", "aborted":
		return "⏹️"
	case "waiting_approval", "waiting_permission", "confirm", "blocked":
		return "⚠️"
	case "waiting_for_input", "idle", "":
		if isError {
			return "❌"
		}
		return "❇️"
	}
	return "[" + state + "]"
}

func millis() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

// clean applies the same text sanitising the status bar uses.
func clean(s string) string {
	s = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ", "\\r", " ", "\\n", " ", "\\t", " ", "\"", "").Replace(s)
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

// fromCache returns the title resolved in a previous invocation.
func fromCache(sessionID string) string {
	if len(sessionID) < 8 {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(CacheDir(), "title_"+sessionID[:8]))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// fromTranscript scans the transcript for the first user request and records a
// negative cache entry when nothing is found.
func fromTranscript(f *agent.Fields) string {
	if len(f.SessionID) < 8 {
		return ""
	}
	short := f.SessionID[:8]
	dir := CacheDir()
	negPath := filepath.Join(dir, "title_neg_"+short)

	if b, err := os.ReadFile(negPath); err == nil {
		var last int64
		fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &last)
		if last > 0 && time.Since(time.Unix(last, 0)) < negativeTTL {
			return ""
		}
	}

	path := f.TranscriptPath
	if path == "" {
		home := os.Getenv("HOME")
		if home == "" {
			home = os.Getenv("USERPROFILE")
		}
		path = filepath.Join(home, ".gemini", "antigravity-cli", "brain", f.SessionID,
			".system_generated", "logs", "transcript.jsonl")
	}
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	title := scanTranscript(path)
	if title == "" {
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(negPath, []byte(fmt.Sprint(time.Now().Unix())), 0o644)
		return ""
	}
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "title_"+short), []byte(title+"\n"), 0o644)
	_ = os.Remove(negPath)
	return title
}

// scanTranscript looks at the first 30 lines for a USER_INPUT entry carrying a
// <USER_REQUEST> block.
func scanTranscript(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) > 30 {
		lines = lines[:30]
	}
	for _, line := range lines {
		if !strings.Contains(line, "\"type\":\"USER_INPUT\"") || !strings.Contains(line, "<USER_REQUEST>") {
			continue
		}
		start := strings.Index(line, "<USER_REQUEST>") + len("<USER_REQUEST>")
		rest := line[start:]
		if end := strings.Index(rest, "</USER_REQUEST>"); end >= 0 {
			rest = rest[:end]
		}
		text := clean(rest)
		if text == "" {
			continue
		}
		return render.TruncHead(text, MaxTitleCols)
	}
	return ""
}
