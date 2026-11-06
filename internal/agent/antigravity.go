package agent

import (
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
		Session:   p.StrAny([]string{"session_name"}, []string{"session_id"}, []string{"conversation_id"}),
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
	f.TranscriptPath = p.Str("transcript_path")

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
