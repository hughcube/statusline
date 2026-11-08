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

func TestWriteCacheAndAcquireLockCreateMissingDir(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "nested", "statusline")
	path := CachePath(cacheDir, "C:/repo")
	if !acquireLock(path) {
		t.Fatal("acquireLock failed when parent cacheDir did not exist yet")
	}
	defer releaseLock(path)

	writeCache(path, Stats{Branch: "main", Dirty: 2, Add: 10, Del: 3})
	got, ts := readCache(path)
	if ts.IsZero() || got.Branch != "main" || got.Dirty != 2 || got.Add != 10 || got.Del != 3 {
		t.Fatalf("readCache = %+v (ts=%v)", got, ts)
	}
}

func TestReadHeadBranchesAndDetached(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 1. Regular branch
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/feature-quick\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b := readHead(dir); b != "feature-quick" {
		t.Errorf("readHead regular = %q, want %q", b, "feature-quick")
	}

	// 2. Detached HEAD
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("0123456789abcdef\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b := readHead(dir); b != "0123456" {
		t.Errorf("readHead detached = %q, want %q", b, "0123456")
	}

	// 3. Worktree via relative gitdir
	wtDir := filepath.Join(dir, "worktrees", "wt1")
	wtRealGit := filepath.Join(dir, ".git", "worktrees", "wt1")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wtRealGit, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, ".git"), []byte("gitdir: ../../.git/worktrees/wt1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtRealGit, "HEAD"), []byte("ref: refs/heads/wt-branch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b := readHead(wtDir); b != "wt-branch" {
		t.Errorf("readHead worktree = %q, want %q", b, "wt-branch")
	}
}

func TestLoadFirstFramePopulatesBranchFromHead(t *testing.T) {
	t.Setenv("STATUSLINE_TESTING", "1")
	cacheDir := t.TempDir()
	repoDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// First frame: no cache file exists yet. Load should populate the branch
	// immediately from HEAD and write cache so subsequent frames hit the cache.
	st := Load(cacheDir, repoDir, repoDir, false)
	if st.Branch != "main" {
		t.Fatalf("first frame branch = %q, want %q", st.Branch, "main")
	}
	cached, ts := readCache(CachePath(cacheDir, repoDir))
	if ts.IsZero() || cached.Branch != "main" {
		t.Fatalf("first frame did not write cache: cached=%+v ts=%v", cached, ts)
	}
}

func TestLoadBranchSwitchInvalidatesStaleCache(t *testing.T) {
	t.Setenv("STATUSLINE_TESTING", "1")
	cacheDir := t.TempDir()
	repoDir := t.TempDir()
	gitDir := filepath.Join(repoDir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/feat-new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pre-populate a cache that is still within TTL, but claiming the branch is "feat-old".
	path := CachePath(cacheDir, repoDir)
	writeCache(path, Stats{Branch: "feat-old", Dirty: 5, Ahead: 1})

	// Load should detect that HEAD is now "feat-new" and discard the stale "feat-old" cache.
	st := Load(cacheDir, repoDir, repoDir, false)
	if st.Branch != "feat-new" {
		t.Fatalf("switched branch = %q, want %q", st.Branch, "feat-new")
	}
	if st.Dirty != 0 {
		t.Fatalf("switched branch dirty = %d, want 0 until refreshed", st.Dirty)
	}
}
