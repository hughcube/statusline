// Package gitstats provides the repository segment: branch, dirty count,
// ahead/behind and line changes.
//
// Constraints inherited from the legacy implementation:
//   - the foreground never blocks: a cache hit is used as-is and an expired
//     cache only triggers a detached refresh;
//   - the cache is keyed by repository root, so several windows looking at the
//     same repo agree on the result;
//   - the cache layout is unchanged for operational compatibility:
//     <epoch>\t<branch>\t<dirty>\t<ahead>\t<behind>\t<add>\t<del>
package gitstats

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hughcube/statusline/internal/pathx"
)

// TTL is the cache lifetime; stale entries are still rendered while a refresh
// runs in the background.
const TTL = 25 * time.Second

// lockTTL bounds how long a refresh may hold the lock before another frame may
// steal it.
const lockTTL = 10 * time.Second

// gitTimeout bounds a single background git invocation so a stalled subprocess
// cannot linger indefinitely.
const gitTimeout = 5 * time.Second

// Stats is the repository state shown in the status line.
type Stats struct {
	Branch string
	Dirty  int
	Ahead  int
	Behind int
	Add    int
	Del    int
}

// Locate walks up from dir looking for .git.
//
// It returns two values:
//   - root: the path exactly as the payload spelled it (MSYS or Windows). The
//     cache file name is derived from it, and it must stay stable or the same
//     repository would end up with two cache entries;
//   - native: the Windows native form used for stat and for git.
//
// Both are empty when no repository is found.
func Locate(dir string) (root, native string) {
	dir = strings.ReplaceAll(dir, "\\", "/")
	for {
		// Stop at the filesystem root. On Windows a bare drive letter is a
		// drive-relative path, so joining it with .git would resolve against
		// the current directory and report an unrelated repository.
		if dir == "" || dir == "/" || (len(dir) == 2 && dir[1] == ':') {
			return "", ""
		}
		if _, err := os.Stat(dir + "/.git"); err == nil {
			return dir, pathx.Native(dir)
		}
		i := strings.LastIndex(dir, "/")
		if i <= 0 {
			return "", ""
		}
		dir = dir[:i]
	}
}

func CachePath(cacheDir, root string) string {
	key := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, root)
	return filepath.Join(cacheDir, key+".git.cache")
}

// Load returns cached stats and schedules a refresh when the entry is stale.
// With sync set (test mode) the refresh happens inline.
func Load(cacheDir, root, native string, sync bool) Stats {
	if root == "" {
		return Stats{}
	}
	path := CachePath(cacheDir, root)
	st, ts := readCache(path)

	head := readHead(native)
	// If HEAD has changed (e.g. user ran git checkout), discard the stale
	// cached branch immediately instead of waiting for the 25s TTL.
	if head != "" && st.Branch != "" && head != st.Branch {
		ts = time.Time{}
		st = Stats{Branch: head}
	} else if st.Branch == "" && head != "" {
		// First frame with no cache yet: populate the branch from HEAD
		// immediately so the status line never renders a blank branch segment.
		st.Branch = head
	}

	if !ts.IsZero() && time.Since(ts) < TTL {
		return st
	}
	if sync || ts.IsZero() {
		st = Fetch(native)
		writeCache(path, st)
		return st
	}
	// Only one frame may own the refresh: without this, a stale cache at 3-5 fps
	// spawns a git process per frame until the worker writes the result.
	if acquireLock(path) {
		refreshAsync(path, native)
	}
	return st
}

func readCache(path string) (Stats, time.Time) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Stats{}, time.Time{}
	}
	fields := strings.Split(strings.TrimRight(string(b), "\r\n"), "\t")
	if len(fields) < 7 {
		return Stats{}, time.Time{}
	}
	sec, _ := strconv.ParseInt(fields[0], 10, 64)
	atoi := func(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }
	return Stats{
		Branch: fields[1],
		Dirty:  atoi(fields[2]),
		Ahead:  atoi(fields[3]),
		Behind: atoi(fields[4]),
		Add:    atoi(fields[5]),
		Del:    atoi(fields[6]),
	}, time.Unix(sec, 0)
}

func writeCache(path string, st Stats) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	line := strings.Join([]string{
		strconv.FormatInt(time.Now().Unix(), 10),
		st.Branch,
		strconv.Itoa(st.Dirty),
		strconv.Itoa(st.Ahead),
		strconv.Itoa(st.Behind),
		strconv.Itoa(st.Add),
		strconv.Itoa(st.Del),
	}, "\t")
	tmp := path + ".tmp"
	if os.WriteFile(tmp, []byte(line), 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}

// refreshAsync spawns a detached worker so the caller returns immediately.
func refreshAsync(path, root string) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "__gitcache", root, path)
	_ = cmd.Start()
	_ = cmd.Process.Release()
}

// CacheWorker is the entry point of the internal __gitcache subcommand.
func CacheWorker(root, cachePath string) {
	defer releaseLock(cachePath)
	writeCache(cachePath, Fetch(root))
}

// acquireLock claims the refresh slot for one repository. The lock is a
// directory so creation is atomic; a stale lock (crashed worker) is stolen
// after lockTTL.
func acquireLock(path string) bool {
	lock := path + ".lock"
	if fi, err := os.Stat(lock); err == nil {
		if time.Since(fi.ModTime()) < lockTTL {
			return false
		}
		_ = os.RemoveAll(lock)
	}
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		return false
	}
	return os.Mkdir(lock, 0o755) == nil
}

func releaseLock(path string) {
	_ = os.RemoveAll(path + ".lock")
}

// Fetch runs git and parses branch, dirty count, ahead/behind and line changes.
func Fetch(root string) Stats {
	var st Stats
	statusCtx, cancelStatus := context.WithTimeout(context.Background(), gitTimeout)
	defer cancelStatus()
	out, err := exec.CommandContext(statusCtx, "git", "-C", root, "--no-optional-locks", "status", "--porcelain=v2", "--branch").Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			switch {
			case strings.HasPrefix(line, "# branch.head "):
				b := strings.TrimSpace(strings.TrimPrefix(line, "# branch.head "))
				if b != "(detached)" {
					st.Branch = b
				}
			case strings.HasPrefix(line, "# branch.ab "):
				// Format: "+2 -1"
				fields := strings.Fields(strings.TrimPrefix(line, "# branch.ab "))
				for _, f := range fields {
					if strings.HasPrefix(f, "+") {
						st.Ahead, _ = strconv.Atoi(f[1:])
					} else if strings.HasPrefix(f, "-") {
						st.Behind, _ = strconv.Atoi(f[1:])
					}
				}
			case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "),
				strings.HasPrefix(line, "u "), strings.HasPrefix(line, "? "):
				st.Dirty++
			}
		}
	}
	if st.Branch == "" {
		st.Branch = readHead(root)
	}
	if st.Dirty > 0 {
		diffCtx, cancelDiff := context.WithTimeout(context.Background(), gitTimeout)
		defer cancelDiff()
		if d, err := exec.CommandContext(diffCtx, "git", "-C", root, "--no-optional-locks", "diff", "HEAD", "--shortstat").Output(); err == nil {
			st.Add, st.Del = parseShortstat(string(d))
		}
	}
	return st
}

// gitDir resolves the real .git directory, supporting gitdir: references in
// worktrees and submodules with absolute or relative paths.
func gitDir(root string) string {
	gd := filepath.Join(root, ".git")
	b, err := os.ReadFile(gd)
	if err != nil {
		return gd
	}
	s := strings.TrimSpace(string(b))
	if strings.HasPrefix(s, "gitdir:") {
		target := strings.TrimSpace(strings.TrimPrefix(s, "gitdir:"))
		if filepath.IsAbs(target) {
			return target
		}
		return filepath.Join(root, target)
	}
	return gd
}

// readHead reads .git/HEAD directly instead of spawning git again.
func readHead(root string) string {
	gd := gitDir(root)
	b, err := os.ReadFile(filepath.Join(gd, "HEAD"))
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(b))
	if strings.HasPrefix(s, "ref: refs/heads/") {
		return strings.TrimPrefix(s, "ref: refs/heads/")
	}
	if len(s) >= 7 {
		return s[:7]
	}
	return s
}

// parseShortstat parses "3 files changed, 12 insertions(+), 4 deletions(-)".
// Above 1000 lines git prints thousands separators ("1,001 insertions"), so the
// number pattern allows commas and they are stripped afterwards.
func parseShortstat(s string) (add, del int) {
	pick := func(re *regexp.Regexp) int {
		m := re.FindStringSubmatch(s)
		if m == nil {
			return 0
		}
		return toInt(strings.ReplaceAll(m[1], ",", ""))
	}
	add = pick(regexp.MustCompile(`([0-9][0-9,]*)\s+insertion`))
	del = pick(regexp.MustCompile(`([0-9][0-9,]*)\s+deletion`))
	return
}

// toInt converts a digit string, returning 0 on garbage.
func toInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
