package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hughcube/statusline/internal/app"
)

// BenchmarkBar measures the renderer only: no process spawn, no shell.
// This is the part a rewrite can actually improve; the rest of a frame is
// operating-system process creation.
func BenchmarkBar(b *testing.B) {
	payloads, err := filepath.Glob(filepath.Join("..", "testdata", "payloads", "*.json"))
	if err != nil || len(payloads) == 0 {
		b.Fatal("no payloads")
	}
	raw, err := os.ReadFile(payloads[0])
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.Bar(raw, "auto", 157)
	}
}
