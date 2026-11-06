// Package tests holds cross-package contract tests.
//
// Golden files under testdata/golden were produced by the legacy shell
// implementation, so these assertions mean "byte-identical to the historical
// behaviour", not "matches whatever the current code happens to print".
package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hughcube/statusline/internal/app"
)

// setupEnv pins every environment input that affects the output.
func setupEnv(t *testing.T) {
	t.Helper()
	// Neutral fake home: golden output must not depend on the machine or the developer.
	t.Setenv("USERPROFILE", "C:/fixture/home")
	t.Setenv("HOME", "C:/fixture/home")
	t.Setenv("HAIKIT_ACCOUNT", "")
	t.Setenv("STATUSLINE_WIDTH", "")
	t.Setenv("STATUSLINE_WIDTH_FILE", "")
	t.Setenv("STATUSLINE_TESTING", "1")
	t.Setenv("AI_CACHE_HOME", t.TempDir())
}

func TestGoldenBars(t *testing.T) {
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
				goldenPath := filepath.Join("..", "testdata", "golden", fmt.Sprintf("%s.%d.txt", name, width))
				want, err := os.ReadFile(goldenPath)
				if err != nil {
					t.Fatalf("cannot read golden: %v", err)
				}
				got := app.Bar(raw, "auto", width).Output + "\n"
				if got != string(want) {
					t.Errorf("output differs from golden\n--- got ---\n%q\n--- want ---\n%q", got, string(want))
				}
			})
		}
	}
}
