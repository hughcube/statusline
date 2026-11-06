# AGENTS.md — debugging the status line in production

This file is for an AI agent (or a maintainer) who has to diagnose a real
status line problem on a user's machine.

## The one hard rule

**Never edit an agent's configuration while debugging.**

That means no changes to `~/.codebuddy/settings.json`, `~/.gemini/antigravity-cli/settings.json`
or any other host config. The only lever you have is **replacing the binary**
the hosts already point at:

| Platform | Deployed path | Note |
| :--- | :--- | :--- |
| Windows | `~/.agents/statusline.exe` | hosts call `~/.agents/statusline`, resolved via `PATHEXT` |
| macOS / Linux | `~/.agents/statusline` | same command, extension-less file |

## Why a special build instead of a runtime switch

The hook runs in a short-lived subprocess whose stderr the host discards, and a
running process's environment cannot be changed from outside. Any "always on"
capture would therefore be dead weight in release builds. Instead, capture lives
behind the `diag` build tag: the release binary contains **none of it**.

## The debug loop

```bash
# 0) work in a scratch copy of the repo; never ship the diag binary
cd ~/Data/ms/statusline

# 1) keep the release binary, so rollback is mechanical
cp ~/.agents/statusline.exe ~/.agents/statusline.exe.release
sha256sum ~/.agents/statusline.exe.release > ~/.agents/statusline.exe.release.sha256

# 2) build and deploy the diagnostic binary
go build -tags diag -ldflags "-s -w -X main.version=diag" -o statusline.exe ./cmd/statusline
cp statusline.exe ~/.agents/statusline.exe
~/.agents/statusline.exe --version      # must print "...+diag"

# 3) let the host render a few frames (2-5 seconds of normal use is enough)

# 4) read what actually happened
cat ~/.cache/statusline/diag/latest.json
ls ~/.cache/statusline/diag/frames/     # ring of the last 50 frames

# 5) fix the code, rebuild the release binary, restore, verify
go build -trimpath -ldflags "-s -w -X main.version=$(git describe --tags --always)" -o statusline.exe ./cmd/statusline
cp ~/.agents/statusline.exe.release ~/.agents/statusline.exe
sha256sum -c ~/.agents/statusline.exe.release.sha256
~/.agents/statusline.exe --version      # must NOT contain "+diag"
```

## What a captured frame contains

```json
{
  "time": "2026-11-07T01:06:33.421+08:00",
  "kind": "bar",
  "build": { "version": "diag", "exe": "C:\\...\\statusline.exe", "size": 3980800, "mod_time": "..." },
  "agent": "codebuddy",
  "width": 134,
  "width_src": "live",
  "duration_ms": 3,
  "payload": { "...": "exactly what the host sent" },
  "output": "\u001b[1G~/.agents ... (exact stdout bytes)",
  "extra": { "Branch": "master", "Dirty": 57, "...": "git segment" }
}
```

Read it in this order: `width_src` -> `width` -> `payload` -> `output`.
That answers "which width won, what did the host send, what did we print".

## Width facts (measured, not guessed)

The binary **cannot** measure the terminal by itself: the host spawns it with
piped stdio and no usable console handle, so `GetConsoleMode` and
`AttachConsole(ATTACH_PARENT_PROCESS)` both fail. Verified experimentally on
Windows 11.

Width therefore comes from outside, in this order:

1. `STATUSLINE_WIDTH_FILE` - a live file the launcher refreshes every second
   (this is what makes resize tracking work, ~0.1 ms per frame to read)
2. `terminal_width` / `columns` in the payload (Antigravity reports it every frame)
3. `STATUSLINE_WIDTH` - launcher snapshot
4. `120` - constant

If `width_src` is `const` or `env` in a captured frame, the launcher is not
injecting the live file; that is a launcher problem, not a renderer problem.


## Launcher contract (codebuddy.py / equivalent wrapper)

The width file is produced outside this repository, so the two sides must agree:

| Side | Responsibility |
| :--- | :--- |
| launcher | writes @@width.<pid>.live@@ atomically **only when the column count changes** (no heartbeat, no periodic writes); injects @@STATUSLINE_WIDTH@@ and @@STATUSLINE_WIDTH_FILE@@ into the CLI environment |
| binary | reads @@STATUSLINE_WIDTH_FILE@@ once per frame (~0.1 ms), strips CR, falls back to the payload, then to @@STATUSLINE_WIDTH@@ |

On startup the launcher prunes its own @@width.*.live@@ files that are both older than
7 days and whose PID is no longer running. It never touches other files in the
cache directory. A missing live file therefore means the launcher died, not that
the renderer failed.
## Symptom -> first action

| Symptom | First action |
| :--- | :--- |
| Status line missing entirely | run `echo "$PAYLOAD" \| ~/.agents/statusline bar --debug` manually; if that works, the host command is wrong |
| Wrong width / wraps oddly | capture a frame, check `width_src` |
| Bar visible but stale | check `STATUSLINE_WIDTH_FILE` mtime; a frozen file means the launcher died |
| Garbled ANSI on a non-CodeBuddy agent | that branch must use `ESC0 + spaces`, never CSI cursor moves |
| Only one agent broken | diff its payload against a working one in `testdata/payloads` |

## Do not

- commit or ship a `diag` binary;
- leave the deployed binary in diag state (always restore + verify the checksum);
- "fix" behaviour by editing an agent's settings during a debugging session.
