// Package widthsource decides how many columns this frame may use.
//
// Lookup order (identical to the legacy implementation; it never probes the
// terminal and never caches anything):
//  1. STATUSLINE_WIDTH_FILE - the live width.<pid>.live file refreshed by the launcher
//  2. terminal_width / columns from the stdin JSON - reported by Antigravity
//  3. STATUSLINE_WIDTH - launcher snapshot
//  4. 120 - degenerate constant
package widthsource

import (
	"os"
	"strconv"
	"strings"
)

// Default is the width used when nothing else is available.
const Default = 120

// Source records where the width came from, for diagnostics.
type Source string

const (
	SourceFile  Source = "live"
	SourceJSON  Source = "json"
	SourceEnv   Source = "env"
	SourceConst Source = "const"
)

// Resolve returns the column count for this frame and its origin.
//
// envWidth takes precedence over STATUSLINE_WIDTH (it is the --width override);
// both rank below the live file and the payload report.
func Resolve(jsonWidth int64, envWidth string) (int, Source) {
	if v, ok := fromFile(os.Getenv("STATUSLINE_WIDTH_FILE")); ok {
		return v, SourceFile
	}
	if jsonWidth > 20 {
		return int(jsonWidth), SourceJSON
	}
	if envWidth == "" {
		envWidth = os.Getenv("STATUSLINE_WIDTH")
	}
	if v, ok := fromEnv(envWidth); ok {
		return v, SourceEnv
	}
	return Default, SourceConst
}

func fromFile(path string) (int, bool) {
	if path == "" {
		return 0, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	// Windows text mode writes CRLF; strip CR before parsing, otherwise the
	// value is silently discarded.
	s := strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 20 {
		return 0, false
	}
	return n, true
}

func fromEnv(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 20 {
		return 0, false
	}
	return n, true
}
