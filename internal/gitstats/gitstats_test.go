package gitstats

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLocateStopsAtDriveRoot guards a Windows trap: a bare drive letter is a
// drive-relative path, so a naive walk would resolve "<drive>:.git" against the
// current directory and report an unrelated repository.
func TestLocateStopsAtDriveRoot(t *testing.T) {
	root, native := Locate("C:/definitely/not/a/repo")
	if root != "" || native != "" {
		t.Fatalf("Locate returned %q/%q for a non-repository path", root, native)
	}
}

func TestLocateFindsRepoRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	root, native := Locate(filepath.ToSlash(dir) + "/sub/dir")
	if root == "" || native == "" {
		t.Fatalf("Locate did not find the repository root for %s", dir)
	}
}

func TestParseShortstat(t *testing.T) {
	cases := []struct {
		in       string
		add, del int
	}{
		{" 3 files changed, 12 insertions(+), 4 deletions(-)", 12, 4},
		{" 1 file changed, 1,001 insertions(+)", 1001, 0},
		{" 1 file changed, 1 insertion(+), 1 deletion(-)", 1, 1},
		{" 1 file changed, 2 deletions(-)", 0, 2},
	}
	for _, c := range cases {
		add, del := parseShortstat(c.in)
		if add != c.add || del != c.del {
			t.Errorf("parseShortstat(%q) = %d/%d, want %d/%d", c.in, add, del, c.add, c.del)
		}
	}
}
