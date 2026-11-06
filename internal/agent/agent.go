// Package agent abstracts the hook payload of different AI CLIs.
//
// Design rules:
//   - one implementation per CLI, responsible only for turning a payload into
//     the shared Fields struct;
//   - the rendering layer (status bar / terminal title) never mentions a CLI;
//   - adding a CLI means adding one file plus one line in the registry.
package agent

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Quota is one quota group: a short window (5h) and a weekly window.
// Used is a percentage in [0,100]; -1 means "window not reported".
type Quota struct {
	Label  string
	Used5  int
	Used7  int
	Reset5 time.Time
	Reset7 time.Time
}

// Fields is the normalized representation every renderer works on.
type Fields struct {
	Dir       string
	Model     string
	Version   string
	Session   string
	SessionID string
	Worktree  string

	InTokens    int64
	OutTokens   int64
	CacheRead   int64
	CacheCreate int64
	CtxSize     int64
	UsedPct     *float64 // window usage computed by CodeBuddy itself

	CostUSD float64
	DurMS   int64

	Effort  string
	Think   string
	Style   string
	Fast    bool
	Sandbox bool

	PlanTier string
	Email    string
	Quotas   []Quota

	// MCPEnabled is the number of MCP servers this directory has turned on.
	// 0 hides the segment, so CLIs without MCP configuration never show it.
	MCPEnabled int

	// Title specific
	State          string
	TranscriptPath string
	IsError        bool
}

// Payload wraps a decoded JSON document.
type Payload struct {
	Root map[string]any
	Raw  string
}

// Parse decodes raw bytes; invalid JSON yields an empty payload instead of an error
// so that a broken payload never blanks the status line.
func Parse(raw []byte) *Payload {
	var v any
	_ = json.Unmarshal(raw, &v)
	root, _ := v.(map[string]any)
	return &Payload{Root: root, Raw: string(raw)}
}

// Get returns the value at path, or nil when any segment is missing.
func (p *Payload) Get(path ...string) any {
	if p == nil || p.Root == nil {
		return nil
	}
	var cur any = p.Root
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[k]
	}
	return cur
}

// Str returns a string value, or "" when missing or not a string.
func (p *Payload) Str(path ...string) string {
	if s, ok := p.Get(path...).(string); ok {
		return s
	}
	return ""
}

// StrAny tries several paths and returns the first non-empty string.
func (p *Payload) StrAny(paths ...[]string) string {
	for _, path := range paths {
		if s := p.Str(path...); s != "" {
			return s
		}
	}
	return ""
}

// Int returns an integer value, or 0 when missing.
func (p *Payload) Int(path ...string) int64 {
	switch v := p.Get(path...).(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	}
	return 0
}

// IntAny tries several paths and returns the first non-zero integer.
func (p *Payload) IntAny(paths ...[]string) int64 {
	for _, path := range paths {
		if n := p.Int(path...); n != 0 {
			return n
		}
	}
	return 0
}

// Float returns a float value and whether it was present.
func (p *Payload) Float(path ...string) (float64, bool) {
	switch v := p.Get(path...).(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	}
	return 0, false
}

// Bool returns a boolean value and whether it was present.
func (p *Payload) Bool(path ...string) (bool, bool) {
	switch v := p.Get(path...).(type) {
	case bool:
		return v, true
	case string:
		if v == "true" {
			return true, true
		}
		if v == "false" {
			return false, true
		}
	}
	return false, false
}

// CalcUsed converts a remaining fraction into a used percentage.
//
// The mapping is intentionally byte-compatible with the legacy shell version:
// "1" and "1.x" become 0, "0" and "0.0" become 100, "0.xx" becomes 100-xx
// (two decimal digits, truncated, never rounded), anything else is -1.
func CalcUsed(remaining float64, ok bool) int {
	if !ok || remaining < 0 {
		return -1
	}
	switch {
	case remaining >= 1:
		return 0
	case remaining <= 0:
		return 100
	}
	s := strconv.FormatFloat(remaining, 'f', 6, 64) // e.g. "0.680000"
	if s == "1.000000" {
		// 0.9999995..1 rounds up while formatting; the shell took the first two
		// decimals ("99") and reported 1%, so keep that behaviour.
		return 1
	}
	if len(s) < 4 || s[0] != '0' || s[1] != '.' {
		return -1
	}
	n, err := strconv.Atoi(s[2:4])
	if err != nil {
		return -1
	}
	return 100 - n
}

// ToInt converts a loosely typed value to int64, falling back to def.
func ToInt(v any, def int64) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil {
			return n
		}
	}
	return def
}
