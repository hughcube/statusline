package render

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/hughcube/statusline/internal/agent"
)

// HomeDir returns the normalized home directory (USERPROFILE first, like the shell version).
func HomeDir() string {
	if h := os.Getenv("USERPROFILE"); h != "" {
		return NormalizePath(h)
	}
	return NormalizePath(os.Getenv("HOME"))
}

// NormalizePath converts backslashes and collapses duplicate separators.
func NormalizePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	return p
}

// DisplayPath abbreviates the home prefix to ~ (drive letter is case-insensitive).
func DisplayPath(dir, home string) string {
	dir = NormalizePath(dir)
	if dir == "" || home == "" {
		return dir
	}
	if strings.HasPrefix(dir, home) {
		return "~" + dir[len(home):]
	}
	if len(dir) > 2 && len(home) > 2 && strings.EqualFold(dir[:2], home[:2]) &&
		len(dir) >= len(home) && dir[2:len(home)] == home[2:] {
		return "~" + dir[len(home):]
	}
	return dir
}

// BaseName returns the last path element.
func BaseName(p string) string {
	p = strings.TrimSuffix(NormalizePath(p), "/")
	if p == "" {
		return ""
	}
	return filepath.Base(p)
}

// Account returns the account label: HAIKIT_ACCOUNT wins, payload email is the fallback.
func Account(f *agent.Fields) string {
	a := os.Getenv("HAIKIT_ACCOUNT")
	if a == "" {
		a = f.Email
	}
	a = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(a)
	for strings.Contains(a, "  ") {
		a = strings.ReplaceAll(a, "  ", " ")
	}
	a = strings.TrimSpace(a)
	if a == "" {
		return ""
	}
	return TruncHead(a, 40)
}

// PlanName shortens known plan names; unknown tiers are shown verbatim.
func PlanName(tier string) string {
	switch tier {
	case "Google AI Pro":
		return "Pro"
	case "Google AI Ultra":
		return "Ultra"
	case "Google AI Ultra 5x":
		return "Ultra 5x"
	case "Google AI Ultra 20x":
		return "Ultra 20x"
	case "Google AI Plus":
		return "Plus"
	}
	return tier
}

var effortWords = map[string]string{
	"high": "high", "low": "low", "medium": "medium", "minimal": "minimal",
	"max": "max", "xhigh": "xhigh",
}

// Normalize fills fields the payload may omit (effort / think):
// infer from model annotations first, then fall back to CodeBuddy settings.json.
func Normalize(f *agent.Fields, client string) *agent.Fields {
	out := *f
	effort := strings.ToLower(strings.TrimSpace(out.Effort))
	think := strings.ToLower(strings.TrimSpace(out.Think))
	out.Effort, out.Think = effort, think

	if client == "codebuddy" && (out.Effort == "" || out.Think == "") {
		re, always := readCodeBuddyThinking()
		if out.Effort == "" && re != "" && re != "auto" {
			out.Effort = strings.ToLower(re)
		}
		if out.Think == "" {
			if always == "false" {
				out.Think = "no-think"
				out.Effort = ""
			} else {
				out.Think = "think"
			}
		}
	}
	if out.Effort == "" {
		for _, w := range parenWords(f.Model) {
			if effortWords[w] != "" {
				out.Effort = w
				break
			}
		}
	}
	if out.Think == "" {
		for _, w := range parenWords(f.Model) {
			switch w {
			case "no-think", "no-thinking", "nothink":
				out.Think = "no-think"
			case "think", "thinking":
				out.Think = "think"
			}
			if out.Think != "" {
				break
			}
		}
	}
	if out.Think == "nothink" {
		out.Think = "no-think"
	}
	if out.Think == "no-think" {
		out.Effort = ""
	}
	return &out
}

// parenWords returns every parenthesized annotation, lowercased, e.g.
// "Gemini 3.7 Flash (High) (Thinking)" -> ["high", "thinking"].
func parenWords(model string) []string {
	var out []string
	for i := 0; i < len(model); i++ {
		if model[i] != '(' {
			continue
		}
		j := strings.IndexByte(model[i:], ')')
		if j < 0 {
			break
		}
		out = append(out, strings.ToLower(strings.TrimSpace(model[i+1:i+j])))
		i += j
	}
	return out
}

// readCodeBuddyThinking reads reasoning settings from ~/.codebuddy/settings.json.
func readCodeBuddyThinking() (effort, always string) {
	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}
	paths := []string{filepath.Join(home, ".codebuddy", "settings.json")}
	if d := os.Getenv("CODEBUDDY_PROJECT_DIR"); d != "" {
		paths = append(paths, filepath.Join(d, ".codebuddy", "settings.json"))
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var cfg struct {
			ReasoningEffort       string `json:"reasoningEffort"`
			AlwaysThinkingEnabled *bool  `json:"alwaysThinkingEnabled"`
		}
		if json.Unmarshal(b, &cfg) != nil {
			continue
		}
		if cfg.ReasoningEffort != "" {
			effort = cfg.ReasoningEffort
		}
		if cfg.AlwaysThinkingEnabled != nil {
			if *cfg.AlwaysThinkingEnabled {
				always = "true"
			} else {
				always = "false"
			}
		}
	}
	return
}

// ModelSeg renders "model capacity flags".
func ModelSeg(f *agent.Fields) string {
	name := cleanModel(f.Model, f.Effort)
	if name == "" {
		name = f.Model
	}
	seg := name
	if f.CtxSize > 0 {
		seg += " " + FmtTokens(f.CtxSize)
	}
	var flags []string
	if f.Effort != "" {
		flags = append(flags, f.Effort)
	}
	if f.Fast {
		flags = append(flags, "fast")
	}
	if f.Think == "think" {
		flags = append(flags, "think")
	}
	if f.Think == "no-think" {
		flags = append(flags, "no-think")
	}
	if f.Sandbox {
		flags = append(flags, "🔒sandbox")
	}
	if f.Style != "" && f.Style != "default" {
		flags = append(flags, "["+f.Style+"]")
	}
	if len(flags) > 0 {
		seg += " " + strings.Join(flags, " ")
	}
	return seg
}

// cleanModel strips capacity annotations ([1m]/[200k]) and consumed parentheses.
func cleanModel(model, effort string) string {
	s := model
	for {
		i := strings.Index(s, "[")
		if i < 0 {
			break
		}
		j := strings.Index(s[i:], "]")
		if j < 0 {
			break
		}
		inner := s[i+1 : i+j]
		if inner != "" && strings.ContainsAny(inner, "0123456789") &&
			(strings.HasSuffix(inner, "m") || strings.HasSuffix(inner, "M") ||
				strings.HasSuffix(inner, "k") || strings.HasSuffix(inner, "K")) {
			s = s[:i] + s[i+j+1:]
			continue
		}
		break
	}
	for {
		i := strings.LastIndex(s, "(")
		if i < 0 || !strings.HasSuffix(s, ")") {
			break
		}
		w := strings.ToLower(strings.TrimSpace(s[i+1 : len(s)-1]))
		if effortWords[w] == "" && w != "thinking" && w != "think" && w != "no-think" &&
			w != "no-thinking" && w != "nothink" && w != effort {
			break
		}
		s = strings.TrimSpace(s[:i])
	}
	for {
		i := strings.Index(s, "(")
		if i < 0 {
			break
		}
		j := strings.IndexByte(s[i:], ')')
		if j < 0 {
			break
		}
		w := strings.ToLower(strings.TrimSpace(s[i+1 : i+j]))
		if effortWords[w] == "" && w != "thinking" && w != "think" && w != "no-think" &&
			w != "no-thinking" && w != "nothink" {
			break
		}
		s = strings.TrimSpace(s[:i] + s[i+j+1:])
	}
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}
