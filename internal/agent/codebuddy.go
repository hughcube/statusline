package agent

import "strings"

// CodeBuddy handles the CodeBuddy / WorkBuddy "Status" hook payload.
//
// Differences from the other CLIs:
//   - window usage comes from used_percentage, computed by CodeBuddy itself;
//   - current_usage.input_tokens is only the non-cached delta under the
//     OpenAI-style usage protocol and must not be treated as window usage;
//   - its renderer wraps by byte width, so callers need both a column and a
//     byte budget.
type CodeBuddy struct{}

func (CodeBuddy) Name() string { return "codebuddy" }

func (CodeBuddy) Detect(p *Payload) bool {
	if p.Str("hook_event_name") == "Status" {
		return true
	}
	return strings.Contains(p.Str("transcript_path"), ".codebuddy")
}

func (CodeBuddy) Parse(p *Payload) *Fields {
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

	// current_usage first, cumulative totals as the fallback.
	f.InTokens = p.Int("context_window", "current_usage", "input_tokens")
	f.OutTokens = p.Int("context_window", "current_usage", "output_tokens")
	if f.InTokens == 0 {
		f.InTokens = p.Int("context_window", "total_input_tokens")
	}
	if f.OutTokens == 0 {
		f.OutTokens = p.Int("context_window", "total_output_tokens")
	}
	f.CacheRead = p.Int("context_window", "current_usage", "cache_read_input_tokens")
	if f.CacheRead == 0 {
		f.CacheRead = p.Int("context_window", "cache_read_input_tokens")
	}
	f.CacheCreate = p.Int("context_window", "current_usage", "cache_creation_input_tokens")
	if f.CacheCreate == 0 {
		f.CacheCreate = p.Int("context_window", "cache_creation_input_tokens")
	}
	f.CtxSize = p.Int("context_window", "context_window_size")
	if v, ok := p.Float("context_window", "used_percentage"); ok {
		f.UsedPct = &v
	}

	f.CostUSD, _ = p.Float("cost", "total_cost_usd")
	f.DurMS = p.Int("cost", "total_duration_ms")
	return f
}
