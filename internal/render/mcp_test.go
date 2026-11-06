package render

import (
	"strings"
	"testing"

	"github.com/hughcube/statusline/internal/agent"
)

func TestMCPSegment(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	f := &agent.Fields{Dir: "C:/work/demo", Model: "Deepseek-V4-Flash", Version: "2.138.0", MCPEnabled: 2}
	out := Build(f, "codebuddy", 157, Git{})
	if !strings.Contains(out, "MCP×2") {
		t.Fatalf("missing MCP segment:\n%s", out)
	}

	// Zero means "nothing is enabled": the segment must disappear entirely.
	f.MCPEnabled = 0
	if strings.Contains(Build(f, "codebuddy", 157, Git{}), "MCP") {
		t.Fatal("MCP segment should be hidden when nothing is enabled")
	}
}

func TestMCPWidthCountsAsSingleColumn(t *testing.T) {
	if got := SegWidth("MCP×1"); got != 5 {
		t.Fatalf("SegWidth(MCP×1) = %d, want 5", got)
	}
}
