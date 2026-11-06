package agent

// Claude handles the Claude Code statusLine payload.
// It has no quota object, only rate_limits with used percentages.
type Claude struct{}

func (Claude) Name() string { return "claude" }

func (Claude) Detect(p *Payload) bool {
	if p.Str("product") == "antigravity" {
		return false
	}
	if p.Get("quota") != nil {
		return false
	}
	return p.Get("workspace") != nil || p.Get("cwd") != nil || p.Get("model") != nil || p.Get("rate_limits") != nil
}

func (Claude) Parse(p *Payload) *Fields {
	f := &Fields{
		Dir:     p.StrAny([]string{"workspace", "current_dir"}, []string{"workspace", "project_dir"}, []string{"cwd"}),
		Model:   p.Str("model", "display_name"),
		Version: p.Str("version"),
		Session: p.StrAny([]string{"session_name"}, []string{"session_id"}),
		Style:   p.Str("output_style", "name"),
	}
	if f.Model == "" {
		f.Model = p.Str("model", "id")
	}
	f.SessionID = p.Str("session_id")
	f.Worktree = p.StrAny([]string{"workspace", "git_worktree"}, []string{"git_worktree"})
	if f.Worktree == "" {
		f.Worktree = p.Str("worktree", "name")
	}

	f.InTokens = p.Int("context_window", "total_input_tokens")
	f.OutTokens = p.Int("context_window", "total_output_tokens")
	f.CacheRead = p.Int("context_window", "current_usage", "cache_read_input_tokens")
	if f.CacheRead == 0 {
		f.CacheRead = p.Int("context_window", "cache_read_input_tokens")
	}
	f.CacheCreate = p.Int("context_window", "current_usage", "cache_creation_input_tokens")
	if f.CacheCreate == 0 {
		f.CacheCreate = p.Int("context_window", "cache_creation_input_tokens")
	}
	f.CtxSize = p.Int("context_window", "context_window_size")

	f.Effort = p.Str("effort", "level")
	f.CostUSD, _ = p.Float("cost", "total_cost_usd")
	f.DurMS = p.Int("cost", "total_duration_ms")

	// rate_limits carry used percentages; truncate like the shell version did.
	if v, ok := p.Float("rate_limits", "five_hour", "used_percentage"); ok {
		f.Quotas = append(f.Quotas, Quota{Label: "", Used5: int(v), Used7: -1})
	}
	if v, ok := p.Float("rate_limits", "seven_day", "used_percentage"); ok {
		if len(f.Quotas) == 0 {
			f.Quotas = append(f.Quotas, Quota{Label: "", Used5: -1})
		}
		f.Quotas[0].Used7 = int(v)
	}
	return f
}
