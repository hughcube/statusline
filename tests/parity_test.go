package tests

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hughcube/statusline/internal/app"
)

// TestParityWithLegacy checks the renderer against the legacy shell
// implementation when STATUSLINE_LEGACY points at it.
//
// Normal runs skip this: testdata/golden already encodes the legacy output, so
// parity is enforced without needing bash. Point the variable at the old
// script when you want to prove the goldens themselves are still faithful
// (e.g. before re-baselining).
// knownDivergences lists frames where the legacy implementation drew a line
// wider than its own container. The Go renderer now fits the line instead of
// copying that overflow, so parity is expected to differ until the legacy
// oracle is retired.
var knownDivergences = map[string]string{
	"p_wt/80": "legacy drew 77 columns into a 76 column container",
}

func TestParityWithLegacy(t *testing.T) {
	script := strings.TrimSpace(os.Getenv("STATUSLINE_LEGACY"))
	if script == "" {
		t.Skip("set STATUSLINE_LEGACY=/path/to/statusline.sh to run parity")
	}
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("legacy script not found: %v", err)
	}

	setupEnv(t)
	payloads, err := filepath.Glob(filepath.Join("..", "testdata", "payloads", "*.json"))
	if err != nil || len(payloads) == 0 {
		t.Fatalf("no payloads found: %v", err)
	}

	for _, payload := range payloads {
		name := strings.TrimSuffix(filepath.Base(payload), ".json")
		raw, err := os.ReadFile(payload)
		if err != nil {
			t.Fatalf("cannot read payload: %v", err)
		}
		for _, width := range []int{80, 120, 157, 200} {
			width := width
			t.Run(fmt.Sprintf("%s/%d", name, width), func(t *testing.T) {
				if why, ok := knownDivergences[fmt.Sprintf("%s/%d", name, width)]; ok {
					t.Skipf("known divergence from the legacy oracle: %s", why)
				}
				cmd := exec.Command("bash", script)
				cmd.Stdin = bytes.NewReader(raw)
				cmd.Env = append(os.Environ(),
					"STATUSLINE_TESTING=1",
					fmt.Sprintf("STATUSLINE_WIDTH=%d", width),
				)
				// The live width file must not leak in: it would pin every frame
				// to the current terminal size.
				cmd.Env = filterEnv(cmd.Env, "STATUSLINE_WIDTH_FILE")

				want, err := cmd.Output()
				if err != nil {
					t.Fatalf("legacy render failed: %v", err)
				}
				got := app.Bar(raw, "auto", width).Output + "\n"
				if got != string(want) {
					t.Errorf("parity mismatch\n--- go ---\n%q\n--- legacy ---\n%q", got, string(want))
				}
			})
		}
	}
}

// filterEnv removes one variable from an environment slice.
func filterEnv(env []string, key string) []string {
	out := env[:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
