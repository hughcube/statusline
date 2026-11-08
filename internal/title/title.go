// Package title produces the terminal window title used by Antigravity and
// other CLIs.
//
// The status glyph comes from the agent_state field supplied by the host; this
// package never guesses. The session title is resolved in this order:
// session_name -> local cache -> first <USER_REQUEST> found in the transcript.
package title

import (
	"bufio"
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

// CacheDir returns the title cache directory, unified under ~/.cache/statusline/title.
func CacheDir() string {
	root := os.Getenv("AI_CACHE_HOME")
	if root == "" {
		home := render.HomeDir()
		root = filepath.Join(home, ".cache")
	}
	return filepath.Join(root, "statusline", "title")
}

// Render builds the title text.
func Render(f *agent.Fields) string {
	project := render.BaseName(f.Dir)
	if project == "" {
		project = "workspace"
	}

	sessionTitle := ""
	if s := clean(f.Session); s != "" && !isSessionID(s, f.SessionID) {
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

// isSessionID reports whether s is a raw machine session/conversation identifier
// (e.g. UUID or matching SessionID) rather than a human-readable title.
func isSessionID(s, sessionID string) bool {
	if s == "" {
		return true
	}
	if sessionID != "" && (s == sessionID || strings.HasPrefix(sessionID, s)) {
		return true
	}
	return isUUID(s)
}

// isUUID checks whether s matches the standard 36-char or 32-char hex UUID format.
func isUUID(s string) bool {
	if len(s) == 36 && s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-' {
		for i, r := range s {
			if i == 8 || i == 13 || i == 18 || i == 23 {
				continue
			}
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
		return true
	}
	if len(s) == 32 {
		for _, r := range s {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
		return true
	}
	return false
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
		base := os.Getenv("AI_APP_DATA_DIR")
		if base == "" {
			base = filepath.Join(render.HomeDir(), ".gemini", "antigravity-cli")
		}
		path = filepath.Join(base, "brain", f.SessionID, ".system_generated", "logs", "transcript.jsonl")
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
//
// The file is streamed: real transcripts grow to tens of megabytes and this
// runs on every render, so loading it whole would dominate the frame cost.
func scanTranscript(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	const maxLines = 30
	const maxLineBytes = 2 << 20
	reader := bufio.NewReaderSize(f, 64<<10)

	for n := 0; n < maxLines; n++ {
		line, readErr := reader.ReadString('\n')
		if len(line) > maxLineBytes {
			line = line[:maxLineBytes]
		}
		if title := matchRequestLine(line); title != "" {
			return title
		}
		if readErr != nil {
			break
		}
	}
	return ""
}

// matchRequestLine extracts the user request body from one transcript line.
func matchRequestLine(line string) string {
	if !strings.Contains(line, "\"type\":\"USER_INPUT\"") {
		return ""
	}
	if strings.Contains(line, "<USER_REQUEST>") {
		start := strings.Index(line, "<USER_REQUEST>") + len("<USER_REQUEST>")
		rest := line[start:]
		if end := strings.Index(rest, "</USER_REQUEST>"); end >= 0 {
			rest = rest[:end]
		}
		text := clean(rest)
		if text != "" {
			return render.TruncHead(text, MaxTitleCols)
		}
	}
	for _, key := range []string{"\"content\":\"", "\"text\":\""} {
		if idx := strings.Index(line, key); idx >= 0 {
			start := idx + len(key)
			rest := line[start:]
			if end := strings.Index(rest, "\""); end >= 0 {
				rest = rest[:end]
			}
			text := clean(rest)
			if text != "" {
				return render.TruncHead(text, MaxTitleCols)
			}
		}
	}
	return ""
}
