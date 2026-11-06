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
cd /path/to/statusline   # the repository, wherever you cloned it

# 1) keep the release binary, so rollback is mechanical
cp ~/.agents/statusline.exe ~/.agents/statusline.exe.release
sha256sum ~/.agents/statusline.exe.release > ~/.agents/statusline.exe.release.sha256

# 2) build and deploy the diagnostic binary
go build -tags diag -ldflags "-s -w -X main.version=diag" -o statusline.exe ./cmd/statusline
cp statusline.exe ~/.agents/statusline.exe
~/.agents/statusline.exe --version      # must print "...+diag"

# 3) let the host render a few frames (2-5 seconds of normal use is enough)

# 4) read what actually happened
cat ~/.cache/statusline/diag/latest.json            # last frame, any agent
ls ~/.cache/statusline/diag/                        # one directory per agent+session
cat ~/.cache/statusline/diag/codebuddy-<sid8>/latest.json
ls ~/.cache/statusline/diag/codebuddy-<sid8>/frames/ # ring of the last 50 frames

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
  "extra": { "Branch": "master", "Dirty": 57, "...": "git segment" },
  "billing": { "Provider": "deepseek", "Label": "DeepSeek", "Amount": "¥10.26", "Stale": false }
}
```

Read it in this order: `width_src` -> `width` -> `payload` -> `output`.
That answers "which width won, what did the host send, what did we print".
`billing` is present only when a credential was discovered; it never contains
the credential.

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
4. `COLUMNS` - injected by the host on every run (Claude Code sets it from the
   TUI's current stdout width; its payload carries no terminal_width)
5. `120` - constant

`width_src` in a captured frame names the winner: `live`, `json`, `env`,
`columns`, or `const`. If it is `const` for a CLI that does report a width,
that is a wiring problem, not a renderer problem. If it is `const` while
`COLUMNS` is set, the renderer is too old to know about that source.


## Launcher contract (codebuddy.py / equivalent wrapper)

The width file is produced outside this repository, so the two sides must agree:

| Side | Responsibility |
| :--- | :--- |
| launcher | writes `width.<pid>.live` atomically **only when the column count changes** (no heartbeat, no periodic writes); injects `STATUSLINE_WIDTH` and `STATUSLINE_WIDTH_FILE` into the CLI environment |
| binary | reads `STATUSLINE_WIDTH_FILE` once per frame (~0.1 ms), strips CR, falls back to the payload, then to `STATUSLINE_WIDTH` |

On startup the launcher prunes its own `width.*.live` files that are both older than
7 days and whose PID is no longer running. It never touches other files in the
cache directory. A missing live file therefore means the launcher died, not that
the renderer failed.

## Billing cache (DeepSeek)

Only Claude Code implements `agent.Billable` today. The balance is discovered
only when `ANTHROPIC_BASE_URL` is the **official** host
(`https://api.deepseek.com`); a relay token is never sent to DeepSeek.

```
<cache>/statusline/billing/deepseek-<sha256:16>.json
<cache>/statusline/billing/deepseek-<sha256:16>.json.lock/
<cache>/statusline/billing/deepseek-<sha256:16>.json.tmp
```

- The name is `sha256("statusline:billing:deepseek:" + key)[:16]`. No key, key
  fragment, e-mail, or base URL appears in the path.
- The file is a JSON envelope: `{"version":1,"fetched_at":...,"last_attempt":...,
  "status":"ok","error":"","response":{...}}`. `response` is the **complete**
  `/user/balance` body, so extra currencies and future fields are preserved.
  It contains no key, request headers, or account identity.
- A failed refresh keeps the last good `response` and only updates
  `last_attempt`, `status`, and `error`.
- The old `.balance` TSV format is intentionally ignored; the next refresh
  rewrites the entry as JSON.
- The foreground only reads the file. A stale entry spawns
  `statusline __billingcache <agent> <path>`; the worker rediscovers the key
  from its inherited environment (never argv), has a 3 s request timeout, and
  writes atomically via `.tmp` + rename.
- Defaults: 60 s TTL (`STATUSLINE_BILLING_TTL` seconds, minimum 30), 60 s error
  backoff, `?` after 2×TTL, hidden after `max(2h, 3×TTL)`.
- `STATUSLINE_BILLING=off` disables it; `STATUSLINE_TESTING=1` disables it in
  tests so golden frames stay deterministic.
- The quota segment (`23%(18:30)/41%(08 09:00)`) is separate: it comes from the
  payload's `rate_limits`, including `resets_at`. The billing amount is inserted
  before it.
- The segment is `DeepSeek ¥10.26` at normal widths; the provider label is
  dropped first when the line gets tight, leaving `¥10.26`.

## Capture layout

Every frame lands in a directory named after the agent and the first eight
characters of the session id, so several CLIs (and several windows of the same
CLI) never overwrite each other:

```
<cache>/statusline/diag/latest.json                     <- last frame overall
<cache>/statusline/diag/<agent>-<sid8>/latest.json      <- last frame of that session
<cache>/statusline/diag/<agent>-<sid8>/frames/NNNNN.json
```

When the payload carries no session id the shard falls back to the process id.
Only `-tags diag` builds write anything; the release binary contains no capture
code at all.

## Testing and re-baselining

```bash
go test ./...                                    # unit + golden + contract
go test ./internal/billing/                      # cache, TTL, worker, HTTP
go test -tags diag ./internal/diag/              # capture channel
STATUSLINE_LEGACY=/path/to/statusline.sh \
  go test ./tests -run TestParityWithLegacy      # byte parity vs legacy shell
go test ./tests -run TestGoldenBars -args -update  # re-baseline (read the warning)
```

`testdata/golden` is the independent oracle: it was produced by the legacy
implementation. Only run `-update` after `TestParityWithLegacy` still passes,
otherwise the goldens degrade into "matches the current code".

## Symptom -> first action

| Symptom | First action |
| :--- | :--- |
| Status line missing entirely | run `echo "$PAYLOAD" \| ~/.agents/statusline bar --debug` manually; if that works, the host command is wrong |
| Wrong width / wraps oddly | capture a frame, check `width_src` |
| Balance missing or stale | run `bar --debug`, inspect `billing`; confirm `ANTHROPIC_BASE_URL` is the official DeepSeek host and check the `.json` cache file's `fetched_at` / `status` / `error` |
| Bar visible but stale | check `STATUSLINE_WIDTH_FILE` mtime; a frozen file means the launcher died |
| Garbled ANSI on a non-CodeBuddy agent | that branch must use `ESC0 + spaces`, never CSI cursor moves |
| Only one agent broken | diff its payload against a working one in `testdata/payloads` |

## Do not

- commit or ship a `diag` binary;
- leave the deployed binary in diag state (always restore + verify the checksum);
- "fix" behaviour by editing an agent's settings during a debugging session.
