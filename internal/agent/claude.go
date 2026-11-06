package agent

import "time"

// Claude handles the Claude Code statusLine payload.
// It has no quota object, only rate_limits with used percentages.
type Claude struct{}

func (Claude) Name() string { return "claude" }

// BillingSources exposes the DeepSeek credential this Claude Code session is
// actually using. The base URL must be the official host; a relay's token is
// never treated as a DeepSeek key. Only the live process environment counts —
// falling back to a settings file could show a different account than the one
// the current session is using.
func (Claude) BillingSources() []BillingSource {
	return deepSeekFromAnthropicEnv("claude")
}

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

	// rate_limits carry used percentages and absolute reset times (Unix
	// seconds). Truncate the percentages like the shell version did, and keep
	// both reset timestamps so the renderer can show them at the levels where
	// the legacy bar showed them.
	q := Quota{Used5: -1, Used7: -1}
	have := false
	if v, ok := p.Float("rate_limits", "five_hour", "used_percentage"); ok {
		q.Used5 = int(v)
		have = true
	}
	if sec := p.Int("rate_limits", "five_hour", "resets_at"); sec > 0 {
		q.Reset5 = time.Unix(sec, 0)
	}
	if v, ok := p.Float("rate_limits", "seven_day", "used_percentage"); ok {
		q.Used7 = int(v)
		have = true
	}
	if sec := p.Int("rate_limits", "seven_day", "resets_at"); sec > 0 {
		q.Reset7 = time.Unix(sec, 0)
	}
	if have {
		f.Quotas = append(f.Quotas, q)
	}
	return f
}
