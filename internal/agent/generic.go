package agent

// Generic is the fallback adapter: unknown CLIs still get a useful line as
// long as they expose the common field names.
type Generic struct{}

func (Generic) Name() string { return "generic" }

func (Generic) Detect(*Payload) bool { return true }

func (Generic) Parse(p *Payload) *Fields {
	f := &Fields{
		Dir:     p.StrAny([]string{"workspace", "current_dir"}, []string{"workspace", "project_dir"}, []string{"cwd"}),
		Model:   p.StrAny([]string{"model", "display_name"}, []string{"model", "id"}),
		Version: p.Str("version"),
		Session: p.StrAny([]string{"session_name"}, []string{"session_id"}, []string{"conversation_id"}),
		State:   p.Str("agent_state"),
	}
	f.SessionID = p.Str("session_id")
	f.InTokens = p.IntAny([]string{"context_window", "total_input_tokens"}, []string{"total_input_tokens"})
	f.OutTokens = p.IntAny([]string{"context_window", "total_output_tokens"}, []string{"total_output_tokens"})
	f.CacheRead = p.IntAny([]string{"context_window", "cache_read_input_tokens"}, []string{"cache_read_input_tokens"})
	f.CacheCreate = p.IntAny([]string{"context_window", "cache_creation_input_tokens"}, []string{"cache_creation_input_tokens"})
	f.CtxSize = p.IntAny([]string{"context_window", "context_window_size"}, []string{"context_window_size"})
	f.DurMS = p.Int("cost", "total_duration_ms")
	f.CostUSD, _ = p.Float("cost", "total_cost_usd")
	return f
}
