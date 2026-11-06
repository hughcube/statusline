// Package mcpcfg counts the MCP servers a CLI has enabled for one directory.
//
// None of the CLIs put MCP state in the status hook payload, so the count has
// to come from the same files the CLI itself reads. Each CLI has its own
// layout and its own notion of "disabled":
//
//	codebuddy    user:    ~/.codebuddy/.mcp.json > ~/.codebuddy/mcp.json >
//	                      ~/.codebuddy.json
//	             project: <dir>/.mcp.json > <dir>/mcp.json
//	             local:   ~/.codebuddy.json projects[<dir>].mcpServers
//	             toggle:  projects[<dir>].disabledMcpServers (user/local
//	                      servers), project file's list (project servers).
//	                      ~/.codebuddy/mcp.json's disabledMcpServers is
//	                      legacy: the CLI never reads it.
//	claude       user:    ~/.claude.json mcpServers, per-entry "disabled"
//	             local:   ~/.claude.json projects[<dir>].mcpServers
//	             project: <dir>/.mcp.json, gated by enabledMcpjsonServers /
//	                      enableAllProjectMcpServers / disabledMcpjsonServers
//	antigravity  ~/.gemini/config/mcp_config.json mcpServers
//
// A result of (0, 0) means "this CLI has no MCP configuration here" and the
// renderer shows nothing; (0, n>0) means every server was turned off.
package mcpcfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Count returns the number of enabled servers and the number of defined ones.
func Count(kind, dir string) (enabled, total int) {
	if dir == "" {
		return 0, 0
	}
	home := homeDir()
	if home == "" {
		return 0, 0
	}
	files := candidateFiles(kind, home, dir)
	if len(files) == 0 {
		return 0, 0
	}
	key := kind + "|" + normKey(dir) + "|" + stamps(files)

	mu.Lock()
	if r, ok := cache[key]; ok {
		mu.Unlock()
		return r.enabled, r.total
	}
	mu.Unlock()

	switch kind {
	case "codebuddy":
		enabled, total = countCodeBuddy(home, dir)
	case "claude":
		enabled, total = countClaude(home, dir)
	case "antigravity":
		enabled, total = countAntigravity(home)
	default:
		return 0, 0
	}

	mu.Lock()
	if len(cache) > 64 {
		cache = map[string]result{}
	}
	cache[key] = result{enabled: enabled, total: total}
	mu.Unlock()
	return enabled, total
}

type result struct{ enabled, total int }

var (
	mu    sync.Mutex
	cache = map[string]result{}
)

// candidateFiles must list exactly the files the count functions read, so the
// cache key changes whenever any of them does.
func candidateFiles(kind, home, dir string) []string {
	switch kind {
	case "codebuddy":
		return []string{
			filepath.Join(home, ".codebuddy", ".mcp.json"),
			filepath.Join(home, ".codebuddy", "mcp.json"),
			filepath.Join(home, ".codebuddy.json"),
			filepath.Join(dir, ".mcp.json"),
			filepath.Join(dir, "mcp.json"),
		}
	case "claude":
		return []string{
			filepath.Join(home, ".claude.json"),
			filepath.Join(dir, ".mcp.json"),
		}
	case "antigravity":
		return []string{filepath.Join(home, ".gemini", "config", "mcp_config.json")}
	}
	return nil
}

func homeDir() string {
	if h := os.Getenv("USERPROFILE"); h != "" {
		return h
	}
	return os.Getenv("HOME")
}

// stamps renders (mtime,size) for every path; a missing file has a stable
// marker of its own so creating one invalidates the cache too.
func stamps(paths []string) string {
	var b strings.Builder
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil {
			b.WriteString(p)
			b.WriteByte('=')
			b.WriteString(st.ModTime().UTC().Format("20060102150405.000000000"))
			b.WriteByte(':')
			b.WriteString(itoa(st.Size()))
			b.WriteByte('|')
			continue
		}
		b.WriteString(p)
		b.WriteString("=-|")
	}
	return b.String()
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// normKey makes project keys comparable: separators unified, case folded, no
// trailing slash. Windows project keys use backslashes and a lowercase drive.
func normKey(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.ToLower(p)
	return strings.TrimSuffix(p, "/")
}

// readJSONC reads a JSONC document (comments and trailing commas allowed).
// A missing or unparsable file yields nil: a broken config must never blank
// the status line.
func readJSONC(path string) map[string]any {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc map[string]any
	if json.Unmarshal(stripJSONC(b), &doc) != nil {
		return nil
	}
	return doc
}

// stripJSONC removes // and /* */ comments plus trailing commas while leaving
// string contents untouched (so "https://..." survives).
func stripJSONC(b []byte) []byte {
	out := make([]byte, 0, len(b))
	inStr, esc := false, false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inStr {
			out = append(out, c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			out = append(out, c)
			continue
		}
		if c == '/' && i+1 < len(b) {
			if b[i+1] == '/' {
				for i < len(b) && b[i] != '\n' {
					i++
				}
				if i < len(b) {
					out = append(out, '\n')
				}
				continue
			}
			if b[i+1] == '*' {
				i += 2
				for i+1 < len(b) && !(b[i] == '*' && b[i+1] == '/') {
					i++
				}
				i++
				continue
			}
		}
		out = append(out, c)
	}
	// Drop a comma whose next non-space byte closes an object or array.
	clean := make([]byte, 0, len(out))
	for i := 0; i < len(out); i++ {
		if out[i] == ',' {
			j := i + 1
			for j < len(out) && (out[j] == ' ' || out[j] == '\t' || out[j] == '\r' || out[j] == '\n') {
				j++
			}
			if j < len(out) && (out[j] == '}' || out[j] == ']') {
				continue
			}
		}
		clean = append(clean, out[i])
	}
	return clean
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func strSet(v any) map[string]bool {
	set := map[string]bool{}
	arr, ok := v.([]any)
	if !ok {
		return set
	}
	for _, item := range arr {
		if s, ok := item.(string); ok {
			set[strings.ToLower(s)] = true
		}
	}
	return set
}

// projectEntry finds projects[<dir>] by comparing normalized keys.
func projectEntry(doc map[string]any, dir string) map[string]any {
	if doc == nil {
		return nil
	}
	projects, ok := doc["projects"].(map[string]any)
	if !ok {
		return nil
	}
	want := normKey(dir)
	for k, v := range projects {
		if normKey(k) != want {
			continue
		}
		if m, ok := v.(map[string]any); ok {
			return m
		}
	}
	return nil
}

// addServers merges one mcpServers object; later calls overwrite the scope of
// an earlier definition with the same name (local > project > user).
func addServers(dst map[string]string, v any, scope string) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	for name := range m {
		dst[strings.ToLower(name)] = scope
	}
}

// firstExisting returns the first path that exists, or "".
func firstExisting(paths ...string) string {
	for _, p := range paths {
		if exists(p) {
			return p
		}
	}
	return ""
}

func countCodeBuddy(home, dir string) (enabled, total int) {
	servers := map[string]string{} // name -> scope

	userFile := firstExisting(
		filepath.Join(home, ".codebuddy", ".mcp.json"),
		filepath.Join(home, ".codebuddy", "mcp.json"),
	)
	if userFile == "" {
		userFile = filepath.Join(home, ".codebuddy.json")
	}
	if doc := readJSONC(userFile); doc != nil {
		addServers(servers, doc["mcpServers"], "user")
	}

	var projectDisabled map[string]bool
	if doc := readJSONC(firstExisting(filepath.Join(dir, ".mcp.json"), filepath.Join(dir, "mcp.json"))); doc != nil {
		projectDisabled = strSet(doc["disabledMcpServers"])
		addServers(servers, doc["mcpServers"], "project")
	}

	local := projectEntry(readJSONC(filepath.Join(home, ".codebuddy.json")), dir)
	localDisabled := map[string]bool{}
	if local != nil {
		localDisabled = strSet(local["disabledMcpServers"])
		addServers(servers, local["mcpServers"], "local")
	}

	for name, scope := range servers {
		off := localDisabled
		if scope == "project" {
			off = projectDisabled
		}
		if !off[name] {
			enabled++
		}
	}
	return enabled, len(servers)
}

func countClaude(home, dir string) (enabled, total int) {
	doc := readJSONC(filepath.Join(home, ".claude.json"))
	local := projectEntry(doc, dir)

	servers := map[string]string{}      // name -> scope
	builtinOff := map[string]bool{}     // per-entry "disabled": true
	localDisabled := map[string]bool{}  // projects[dir].disabledMcpServers
	projectDenied := map[string]bool{}  // projects[dir].disabledMcpjsonServers
	projectAllowed := map[string]bool{} // projects[dir].enabledMcpjsonServers
	allProject := false

	if doc != nil {
		if m, ok := doc["mcpServers"].(map[string]any); ok {
			for name, raw := range m {
				addServers(servers, map[string]any{name: raw}, "user")
				if entry, ok := raw.(map[string]any); ok {
					if d, ok := entry["disabled"].(bool); ok && d {
						builtinOff[strings.ToLower(name)] = true
					}
				}
			}
		}
		if v, ok := doc["enableAllProjectMcpServers"].(bool); ok && v {
			allProject = true
		}
	}
	if local != nil {
		localDisabled = strSet(local["disabledMcpServers"])
		projectDenied = strSet(local["disabledMcpjsonServers"])
		projectAllowed = strSet(local["enabledMcpjsonServers"])
		if v, ok := local["enableAllProjectMcpServers"].(bool); ok && v {
			allProject = true
		}
		addServers(servers, local["mcpServers"], "local")
	}
	if pdoc := readJSONC(filepath.Join(dir, ".mcp.json")); pdoc != nil {
		addServers(servers, pdoc["mcpServers"], "project")
	}

	for name, scope := range servers {
		if builtinOff[name] {
			continue
		}
		switch scope {
		case "project":
			if !allProject && !projectAllowed[name] {
				continue
			}
			if projectDenied[name] {
				continue
			}
		default:
			if localDisabled[name] {
				continue
			}
		}
		enabled++
	}
	return enabled, len(servers)
}

func countAntigravity(home string) (enabled, total int) {
	doc := readJSONC(filepath.Join(home, ".gemini", "config", "mcp_config.json"))
	if doc == nil {
		return 0, 0
	}
	m, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		return 0, 0
	}
	for _, raw := range m {
		off := false
		if entry, ok := raw.(map[string]any); ok {
			if d, ok := entry["disabled"].(bool); ok && d {
				off = true
			}
		}
		if !off {
			enabled++
		}
	}
	return enabled, len(m)
}
