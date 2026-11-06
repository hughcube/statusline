package pathx

import (
	"os"
	"strings"
	"testing"
)

func TestNative(t *testing.T) {
	if got := Native("C:/Users/x"); got != "C:/Users/x" {
		t.Errorf("native path must pass through: %q", got)
	}
	if got := Native("/c/Users/x"); got != "C:/Users/x" {
		t.Errorf("/c/ form: %q", got)
	}
	if got := Native("/tmp"); got != os.TempDir() {
		t.Errorf("/tmp mapping: %q", got)
	}
	if got := Native("/tmp/a/b"); !strings.HasSuffix(got, "/a/b") {
		t.Errorf("/tmp prefix mapping: %q", got)
	}
	if got := Native(""); got != "" {
		t.Errorf("empty must stay empty: %q", got)
	}
}
