// Package app wires "read payload -> pick agent -> render" together so the
// command line stays a thin argument parser and tests can call it directly.
package app

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/hughcube/statusline/internal/agent"
	"github.com/hughcube/statusline/internal/gitstats"
	"github.com/hughcube/statusline/internal/pathx"
	"github.com/hughcube/statusline/internal/render"
	"github.com/hughcube/statusline/internal/title"
	"github.com/hughcube/statusline/internal/widthsource"
)

// BarResult is everything one rendering produced, for --debug and for tests.
type BarResult struct {
	Output    string
	Agent     string
	ProbeW    int
	WidthFrom widthsource.Source
	Fields    *agent.Fields
	Git       render.Git
}

// Bar renders the two status lines. widthOverride > 20 forces a width
// (debug and tests only).
func Bar(raw []byte, agentName string, widthOverride int) BarResult {
	p := agent.Parse(raw)
	a := agent.Resolve(agentName, p)
	f := a.Parse(p)

	envWidth := ""
	if widthOverride > 20 {
		envWidth = strconv.Itoa(widthOverride)
	}
	probeW, src := widthsource.Resolve(jsonWidth(p, a.Name()), envWidth)

	var git render.Git
	if root, native := gitstats.Locate(f.Dir); root != "" {
		st := gitstats.Load(CacheDir(), root, native, os.Getenv("STATUSLINE_TESTING") == "1")
		git = render.Git{Branch: st.Branch, Dirty: st.Dirty, Ahead: st.Ahead, Behind: st.Behind, Add: st.Add, Del: st.Del}
	}

	return BarResult{
		Output:    render.Build(f, a.Name(), probeW, git),
		Agent:     a.Name(),
		ProbeW:    probeW,
		WidthFrom: src,
		Fields:    f,
		Git:       git,
	}
}

// Title renders the terminal title, returning the output, the resolved agent
// name and the normalized fields.
func Title(raw []byte, agentName string) (string, string, *agent.Fields) {
	p := agent.Parse(raw)
	a := agent.Resolve(agentName, p)
	f := a.Parse(p)
	// Title semantics are CLI independent: state and transcript come straight
	// from the payload rather than from an adapter's status bar focus.
	f.State = p.Str("agent_state")
	if f.TranscriptPath == "" {
		f.TranscriptPath = p.Str("transcript_path")
	}
	f.IsError = HasError(p)
	return title.Render(f), a.Name(), f
}

// jsonWidth reads the terminal width reported in the payload.
//
// Antigravity reports terminal_width on every frame; when it is missing the
// historical behaviour is to fall back to 120, which also wins over
// STATUSLINE_WIDTH because that CLI can always measure itself. Other agents
// leave this at 0 so widthsource can fall through to COLUMNS or the launcher.
func jsonWidth(p *agent.Payload, name string) int64 {
	w := int64(0)
	if v, ok := p.Get("terminal_width").(float64); ok {
		w = int64(v)
	}
	if w <= 20 {
		if v, ok := p.Get("columns").(float64); ok {
			w = int64(v)
		}
	}
	if name == "antigravity" && w <= 20 {
		w = 120
	}
	return w
}

// CacheDir returns the shared cache directory (AI_CACHE_HOME wins), matching
// the shell implementation.
func CacheDir() string {
	root := os.Getenv("AI_CACHE_HOME")
	if root == "" {
		home := os.Getenv("HOME")
		if home == "" {
			home = os.Getenv("USERPROFILE")
		}
		root = filepath.Join(pathx.Native(home), ".cache")
	}
	dir := filepath.Join(root, "statusline")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// HasError reports whether the payload says an error just happened, which
// turns the title glyph into ❌.
func HasError(p *agent.Payload) bool {
	if v, ok := p.Bool("is_error"); ok && v {
		return true
	}
	if v, ok := p.Bool("has_error"); ok && v {
		return true
	}
	if e := p.Get("error"); e != nil {
		if s, ok := e.(string); !ok || s != "" {
			return true
		}
	}
	return false
}
