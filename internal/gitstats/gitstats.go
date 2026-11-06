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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hughcube/statusline/internal/pathx"
)

// TTL is the cache lifetime; stale entries are still rendered while a refresh
// runs in the background.
const TTL = 25 * time.Second

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
		if dir == "" || dir == "/" {
			return "", ""
		}
		if _, err := os.Stat(filepath.Join(pathx.Native(dir), ".git")); err == nil {
			return dir, pathx.Native(dir)
		}
		i := strings.LastIndex(dir, "/")
		if i <= 0 {
			return "", ""
		}
		dir = dir[:i]
	}
}

// CachePath returns the cache file for a repository root.
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
	if time.Since(ts) < TTL {
		return st
	}
	if sync {
		st = Fetch(native)
		writeCache(path, st)
		return st
	}
	refreshAsync(path, native)
	return st
}

func readCache(path string) (Stats, time.Time) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Stats{}, time.Unix(0, 0)
	}
	fields := strings.Split(strings.TrimRight(string(b), "\r\n"), "\t")
	if len(fields) < 7 {
		return Stats{}, time.Unix(0, 0)
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
	writeCache(cachePath, Fetch(root))
}

// Fetch runs git and parses branch, dirty count, ahead/behind and line changes.
func Fetch(root string) Stats {
	var st Stats
	out, err := exec.Command("git", "-C", root, "--no-optional-locks", "status", "--porcelain=v2", "--branch").Output()
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
		if d, err := exec.Command("git", "-C", root, "--no-optional-locks", "diff", "HEAD", "--shortstat").Output(); err == nil {
			st.Add, st.Del = parseShortstat(string(d))
		}
	}
	return st
}

// readHead reads .git/HEAD directly instead of spawning git again.
func readHead(root string) string {
	gitDir := filepath.Join(root, ".git")
	if b, err := os.ReadFile(gitDir); err == nil {
		if s := strings.TrimSpace(string(b)); strings.HasPrefix(s, "gitdir:") {
			gitDir = strings.TrimSpace(strings.TrimPrefix(s, "gitdir:"))
		}
	}
	b, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
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
// Above 1000 lines git prints thousands separators ("1,234 insertions"), so
// the digits must be collected explicitly.
func parseShortstat(s string) (add, del int) {
	num := func(part string) int {
		var digits strings.Builder
		for _, r := range part {
			if r >= '0' && r <= '9' {
				digits.WriteRune(r)
			}
		}
		n, _ := strconv.Atoi(digits.String())
		return n
	}
	if i := strings.Index(s, "insertion"); i > 0 {
		head := s[:i]
		if j := strings.LastIndex(head, ","); j >= 0 {
			head = head[j+1:]
		}
		add = num(head)
	}
	if i := strings.Index(s, "deletion"); i > 0 {
		head := s[:i]
		if j := strings.LastIndex(head, ","); j >= 0 {
			head = head[j+1:]
		}
		del = num(head)
	}
	return
}
