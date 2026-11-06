package agent

// Agent describes one CLI payload shape.
//
// An implementation only has to turn a payload into Fields; output formatting
// is decided by the caller, which is what lets a single agent feed both the
// status bar and the terminal title.
type Agent interface {
	// Name is the CLI identifier used by --agent.
	Name() string
	// Detect reports whether the payload belongs to this CLI.
	Detect(p *Payload) bool
	// Parse normalizes the payload into Fields.
	Parse(p *Payload) *Fields
}

// registry order is the auto-detection order: specific first, generic last.
var registry = []Agent{
	CodeBuddy{},
	Antigravity{},
	Claude{},
	Generic{},
}

// ByName looks an agent up by name; nil when unknown.
func ByName(name string) Agent {
	for _, a := range registry {
		if a.Name() == name {
			return a
		}
	}
	return nil
}

// Detect picks an agent by payload shape, falling back to generic.
func Detect(p *Payload) Agent {
	for _, a := range registry {
		if a.Name() == "generic" {
			return a
		}
		if a.Detect(p) {
			return a
		}
	}
	return registry[len(registry)-1]
}

// Resolve handles the --agent flag: empty or "auto" means detection, and the
// short aliases keep old command lines working.
func Resolve(name string, p *Payload) Agent {
	switch name {
	case "", "auto":
		return Detect(p)
	case "cb":
		name = "codebuddy"
	case "agy", "gemini":
		name = "antigravity"
	case "cc":
		name = "claude"
	}
	if a := ByName(name); a != nil {
		return a
	}
	return Detect(p)
}
