// Package app wires "read payload -> pick agent -> render" together so the
// command line stays a thin argument parser and tests can call it directly.
package app

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/hughcube/statusline/internal/agent"
	"github.com/hughcube/statusline/internal/billing"
	"github.com/hughcube/statusline/internal/gitstats"
	"github.com/hughcube/statusline/internal/mcpcfg"
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
	Billing   billing.State
}

// Bar renders the two status lines without the MCP segment. widthOverride >
// 20 forces a width (debug and tests only).
func Bar(raw []byte, agentName string, widthOverride int) BarResult {
	return BarWith(raw, agentName, widthOverride, false)
}

// BarWith renders the two status lines. mcpEnabled adds the "MCP×N" segment,
// which has to be read from the CLI's own configuration files.
func BarWith(raw []byte, agentName string, widthOverride int, mcpEnabled bool) BarResult {
	p := agent.Parse(raw)
	a := agent.Resolve(agentName, p)
	f := a.Parse(p)
	if mcpEnabled {
		f.MCPEnabled, _ = mcpcfg.Count(a.Name(), f.Dir)
	}

	// Share the unified title cache with the status bar: if f.Session is a raw
	// machine ID/UUID and the title cache exists, render the real title.
	// To keep status bar rendering instantaneous (<1ms) on new sessions, the
	// foreground Bar never blocks on transcript disk parsing; that is left to
	// the window title runner or subsequent frames once cached.
	if title.IsSessionID(f.Session, f.SessionID) {
		if t := title.FromCache(f.SessionID); t != "" {
			f.Session = t
		}
	}

	envWidth := ""
	if widthOverride > 20 {
		envWidth = strconv.Itoa(widthOverride)
	}
	probeW, src := widthsource.Resolve(jsonWidth(p, a.Name()), envWidth)

	var cacheDir string
	getCacheDir := func() string {
		if cacheDir == "" {
			cacheDir = CacheDir()
		}
		return cacheDir
	}

	var git render.Git
	if root, native := gitstats.Locate(f.Dir); root != "" {
		st := gitstats.Load(getCacheDir(), root, native, os.Getenv("STATUSLINE_TESTING") == "1")
		git = render.Git{Branch: st.Branch, Dirty: st.Dirty, Ahead: st.Ahead, Behind: st.Behind, Add: st.Add, Del: st.Del}
	}

	// The balance is a cache read only; a miss or an expired entry schedules a
	// detached worker and this frame renders the previous value immediately.
	var bstate billing.State
	bill := render.Billing{}
	if !billing.Disabled() && len(billing.SourcesFor(a.Name())) > 0 {
		bstate = billing.Load(getCacheDir(), a.Name())
		bill = render.Billing{
			Label:    bstate.Label,
			Text:     bstate.Amount,
			Stale:    bstate.Stale,
			Account:  bstate.Account,
			Expiring: bstate.Expiring,
		}
	}

	return BarResult{
		Output:    render.BuildWith(f, a.Name(), probeW, git, bill),
		Agent:     a.Name(),
		ProbeW:    probeW,
		WidthFrom: src,
		Fields:    f,
		Git:       git,
		Billing:   bstate,
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
// the shell implementation. Directory creation is deferred to cache writers
// so read-only frames avoid redundant filesystem metadata calls.
func CacheDir() string {
	root := os.Getenv("AI_CACHE_HOME")
	if root == "" {
		home := os.Getenv("HOME")
		if home == "" {
			home = os.Getenv("USERPROFILE")
		}
		root = filepath.Join(pathx.Native(home), ".cache")
	}
	return filepath.Join(root, "statusline")
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
