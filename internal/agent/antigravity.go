package agent

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Antigravity handles the Google Antigravity CLI hook payload.
//
// Its quota object comes in three shapes and all of them must work:
//   - two groups: 3p-5h/3p-weekly plus gemini-5h/gemini-weekly
//   - three groups: additionally gemini-argon*-5h / gemini-argon*-weekly
//   - single level: quota carries remaining_fraction directly
type Antigravity struct{}

func (Antigravity) Name() string { return "antigravity" }

func (Antigravity) Detect(p *Payload) bool {
	if p.Str("product") == "antigravity" {
		return true
	}
	if q, ok := p.Get("quota").(map[string]any); ok && len(q) > 0 {
		return true
	}
	return false
}

// Parse normalizes the payload. Token counts always use the cumulative totals,
// matching the historical shell behaviour.
func (Antigravity) Parse(p *Payload) *Fields {
	f := &Fields{
		Dir:       p.StrAny([]string{"workspace", "current_dir"}, []string{"workspace", "project_dir"}, []string{"cwd"}),
		Model:     p.Str("model", "display_name"),
		Version:   p.Str("version"),
		SessionID: p.Str("session_id"),
		PlanTier:  p.Str("plan_tier"),
		Email:     p.Str("email"),
		Style:     p.Str("output_style", "name"),
		State:     p.Str("agent_state"),
		Worktree:  p.Str("worktree", "name"),
	}
	if f.Model == "" {
		f.Model = p.Str("model", "id")
	}
	if f.SessionID == "" {
		f.SessionID = p.Str("conversation_id")
	}

	// Antigravity persists human-readable conversation titles directly in
	// ~/.gemini/antigravity-cli/annotations/<sid>.pbtxt as title:"...".
	// Direct read is microsecond-fast (~0.05ms) and needs no secondary cache.
	// If missing, fall back to payload session_name (if not a raw UUID); never
	// fall back to machine UUIDs.
	if t := readAntigravityTitle(f.SessionID); t != "" {
		f.Session = t
	} else if s := p.Str("session_name"); s != "" && !isUUID(s) {
		f.Session = s
	}

	f.InTokens = p.Int("context_window", "total_input_tokens")
	f.OutTokens = p.Int("context_window", "total_output_tokens")
	f.CacheRead = p.Int("context_window", "cache_read_input_tokens")
	f.CacheCreate = p.Int("context_window", "cache_creation_input_tokens")
	f.CtxSize = p.Int("context_window", "context_window_size")

	f.Effort = p.Str("effort", "level")
	if f.Effort == "" {
		f.Effort = p.Str("model", "effort")
	}
	if on, ok := p.Bool("thinking", "enabled"); ok {
		if on {
			f.Think = "think"
		} else {
			f.Think = "no-think"
		}
	}
	if v, ok := p.Bool("fast_mode"); ok {
		f.Fast = v
	}
	if v, ok := p.Bool("sandbox", "enabled"); ok {
		f.Sandbox = v
	}
	f.CostUSD, _ = p.Float("cost", "total_cost_usd")
	f.DurMS = p.Int("cost", "total_duration_ms")

	f.Quotas = parseAntigravityQuotas(p)
	return f
}

// parseAntigravityQuotas flattens the three quota shapes into one list.
func parseAntigravityQuotas(p *Payload) []Quota {
	q, _ := p.Get("quota").(map[string]any)
	if q == nil {
		return nil
	}
	now := time.Now()
	var out []Quota

	build := func(label, h5Key, w7Key, prefix string) {
		var h5, w7 map[string]any
		if h5Key != "" {
			h5, _ = q[h5Key].(map[string]any)
		}
		if w7Key != "" {
			w7, _ = q[w7Key].(map[string]any)
		}
		if prefix != "" {
			for k, v := range q {
				m, ok := v.(map[string]any)
				if !ok || !strings.HasPrefix(k, prefix) {
					continue
				}
				if strings.HasSuffix(k, "-5h") {
					h5 = m
				} else if strings.HasSuffix(k, "-weekly") {
					w7 = m
				}
			}
		}
		if h5 == nil && w7 == nil {
			return
		}
		g := Quota{Label: label, Used5: -1, Used7: -1}
		if h5 != nil {
			rf, ok := h5["remaining_fraction"].(float64)
			g.Used5 = CalcUsed(rf, ok)
			if s, ok := h5["reset_in_seconds"].(float64); ok && s > 0 {
				g.Reset5 = now.Add(time.Duration(s) * time.Second)
			}
		}
		if w7 != nil {
			rf, ok := w7["remaining_fraction"].(float64)
			g.Used7 = CalcUsed(rf, ok)
			if s, ok := w7["reset_in_seconds"].(float64); ok && s > 0 {
				g.Reset7 = now.Add(time.Duration(s) * time.Second)
			}
		}
		out = append(out, g)
	}

	build("P", "3p-5h", "3p-weekly", "")
	build("G", "gemini-5h", "gemini-weekly", "")
	build("G4", "", "", "gemini-argon")

	if len(out) == 0 {
		if rf, ok := q["remaining_fraction"].(float64); ok {
			g := Quota{Label: "G", Used5: CalcUsed(rf, true), Used7: -1}
			if s, ok := q["reset_in_seconds"].(float64); ok && s > 0 {
				g.Reset5 = now.Add(time.Duration(s) * time.Second)
			}
			out = append(out, g)
		}
	}
	return out
}

// readAntigravityTitle reads the session title directly from the Antigravity
// annotations file (~/.gemini/antigravity-cli/annotations/<sid>.pbtxt).
func readAntigravityTitle(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	base := os.Getenv("AI_APP_DATA_DIR")
	if base == "" {
		if os.Getenv("STATUSLINE_TESTING") == "1" {
			return ""
		}
		home := os.Getenv("USERPROFILE")
		if home == "" {
			home = os.Getenv("HOME")
		}
		base = filepath.Join(home, ".gemini", "antigravity-cli")
	}
	path := filepath.Join(base, "annotations", sessionID+".pbtxt")
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := string(b)
	idx := strings.Index(s, "title:")
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(s[idx+len("title:"):])
	if strings.HasPrefix(rest, "\"") {
		rest = rest[1:]
		if end := strings.Index(rest, "\""); end >= 0 {
			return strings.TrimSpace(rest[:end])
		}
	}
	return ""
}

// isUUID checks whether s matches the standard 36-char or 32-char hex UUID format.
func isUUID(s string) bool {
	if len(s) == 36 && s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-' {
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
