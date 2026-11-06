# statusline — One Status Bar and Terminal Title for Every AI Coding Agent

[![CI](https://github.com/hughcube/statusline/actions/workflows/ci.yml/badge.svg)](https://github.com/hughcube/statusline/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/hughcube/statusline)](https://github.com/hughcube/statusline/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

English | [中文](README_zh.md)

**One binary, every agent's status line.** `statusline` renders the two-line footer
and the terminal title that AI coding agents display, using a single Go binary that
replaces the per-agent Bash/Perl scripts. The same output runs unchanged across
**CodeBuddy / WorkBuddy**, **Google Antigravity**, **Claude Code** and any future agent
that can pipe a JSON payload into a command.

---

## Table of Contents

- [Why statusline?](#why-statusline)
- [Supported agents](#supported-agents)
- [Features](#features)
- [Install](#install)
- [Configuration](#configuration)
- [DeepSeek balance](#deepseek-balance)
- [Width contract](#width-contract)
- [Performance](#performance)
- [Debugging](#debugging)
- [Testing](#testing)
- [License](#license)

---

## Why statusline?

| Aspect | One script per agent | `statusline` |
| :--- | :--- | :--- |
| **Agents** | Bash for one CLI, Perl for another, drifting apart | One binary, adapters behind an interface |
| **Platforms** | Needs bash/perl present (macOS keeps dropping runtimes) | Static binary, no runtime dependency |
| **Width** | Each script probes the terminal differently | One contract, byte + column budgets |
| **Tests** | Shell assertions, hard to run in CI | `go test`: unit, golden, contract |
| **Debugging** | Print and pray | `--debug` dumps the parsed payload to stderr |

The renderer is deliberately dumb: it receives normalized fields and produces
two strings. Everything agent-specific lives in `internal/agent`, so a new CLI
is one file plus one registry line.

## Supported agents

| Agent | Status bar | Terminal title | Notes |
| :--- | :---: | :---: | :--- |
| CodeBuddy / WorkBuddy | yes | — | window usage from `used_percentage`, byte-budget aware |
| Google Antigravity | yes | yes | quota groups `P` / `G` / `G4`, animated state icon |
| Claude Code | yes | — | `rate_limits` with reset times; DeepSeek balance |
| anything else | best effort | — | generic adapter |

## Features

- **One binary, every agent**: Claude Code, CodeBuddy / WorkBuddy, Google Antigravity.
- **DeepSeek balance**: official `/user/balance`, shown as `DeepSeek ¥10.26`; `!` for insufficient balance, `?` for a stale cache.
- **Quotas with reset times**: five-hour and seven-day windows from `rate_limits`.
- **Tokens and cost**: In / Out / Cache / CacheWrite, context usage, session cost and duration.
- **Git context**: branch, dirty files, ahead/behind, added/deleted lines, worktree.
- **Width contract**: live width file, payload width, `STATUSLINE_WIDTH`, `COLUMNS`, plus a byte budget for CodeBuddy.
- **Non-blocking foreground**: git and balance refresh in detached workers; the bar only reads cache.
- **Zero runtime dependencies**: one static binary, same config on Windows / macOS / Linux.

## Install

Download the artifact for your platform from
[releases](https://github.com/hughcube/statusline/releases) and place it at
`~/.agents/statusline`. On Windows the file is `statusline.exe`; the command
`~/.agents/statusline` still resolves to it through `PATHEXT`, so the same
configuration works on every platform.

From source:

```bash
make build      # native binary for this machine
make cross      # all six release artifacts under dist/
```

## Configuration

CodeBuddy (`~/.codebuddy/settings.json`):

```json
{
  "statusLine": {
    "type": "command",
    "command": "~/.agents/statusline bar --agent codebuddy",
    "padding": 0
  }
}
```

Antigravity:

```json
{
  "statusLine": { "type": "command", "command": "~/.agents/statusline bar --agent antigravity" },
  "title":      { "type": "command", "command": "~/.agents/statusline title --agent antigravity" }
}
```

Claude Code:

```json
{
  "statusLine": { "type": "command", "command": "~/.agents/statusline bar --agent claude" }
}
```

## DeepSeek balance

When Claude Code runs against the **official** DeepSeek endpoint
(`ANTHROPIC_BASE_URL=https://api.deepseek.com/...`), line 2 inserts the current
session key's balance before the quota segment:

```text
DeepSeek ¥10.26 | 23%(18:30)/41%(08 09:00) | deepseek-flash 1.0M | v2.1.0
```

The value comes from the official `GET https://api.deepseek.com/user/balance`;
only `total_balance` is rendered. DeepSeek has no public cost/usage API, so the
bar does not show daily or monthly spend.

**Display rules**

| State | Output |
| :--- | :--- |
| Normal | `DeepSeek ¥10.26` |
| Account cannot call the API | `DeepSeek ¥0.00!` |
| Cache older than 2×TTL, or refresh failed | `DeepSeek ¥10.26?` |
| Older than `max(2h, 3×TTL)` | balance hidden |
| Narrow width | drop `DeepSeek` first (`¥10.26`), then hide the amount with the account context |

**Detection and credential**

- Only `https://api.deepseek.com` is accepted (exact hostname match); a relay's
  token is never sent to DeepSeek.
- Uses the current Claude Code session's `ANTHROPIC_AUTH_TOKEN` (then
  `ANTHROPIC_API_KEY`). Different keys use different cache files, so switching
  accounts never mixes balances.
- The account segment (`HAIKIT_ACCOUNT` or payload `email`) is independent of
  the balance. DeepSeek's balance API does not return an account name, so a
  Claude Code session usually shows only `DeepSeek ¥10.26`.

**Cache and performance**

- Cache: `~/.cache/statusline/billing/deepseek-<sha256:16>.json`, named from
  `sha256("statusline:billing:deepseek:" + key)`. The name contains no key, key
  fragment, e-mail, or base URL.
- The cache stores the **complete `/user/balance` response JSON** plus local
  metadata, not just today's display fields, so extra currencies and future
  fields are never lost. It contains no key, request headers, or account data.
- The foreground reads one small JSON file (measured **~50 µs**) and never
  performs network I/O. An expired entry starts a detached worker (3 s request
  timeout); the current frame returns the previous value immediately.
- Default refresh interval 60 s; failures back off 60 s; `?` appears after
  2×TTL; the value is hidden after `max(2h, 3×TTL)`.

**Environment variables**

| Variable | Default | Meaning |
| :--- | ---: | :--- |
| `STATUSLINE_BILLING_TTL` | `60` | successful refresh interval in seconds; minimum 30; invalid falls back to 60 |
| `STATUSLINE_BILLING` | on | `off` / `0` / `false` / `no` disables billing |
| `STATUSLINE_TESTING` | — | `1` disables billing so tests stay deterministic |

**Scope**

Only Claude Code is wired today; `agent.Billable` is the extension point, so
CodeBuddy / Antigravity can be added with one method.

## Width contract

The status bar never probes the terminal. Width is resolved in this order:

1. `STATUSLINE_WIDTH_FILE` — a live file refreshed by the launcher (follows resizes)
2. `terminal_width` / `columns` in the payload (Antigravity reports it every frame)
3. `STATUSLINE_WIDTH` — launcher snapshot (an explicit `--width` wins over it)
4. `COLUMNS` — set by the host on every run; Claude Code derives it from the TUI,
   because its payload carries no terminal width
5. `120` — fallback

The winning source is reported as `width_src` in a diagnostic build: `live`,
`json`, `env`, `columns` or `const`.

Container widths: codebuddy `W-1`, antigravity `W`, others `W-4`.

CodeBuddy additionally gets a **byte budget**: its renderer wraps by bytes while
the terminal measures columns, and CJK characters are 2 columns but 3 bytes.

## Performance

Measured on Windows 11, 15 runs, payload in, two lines out (antivirus disabled; with a
security product active, a freshly downloaded binary is scanned on every launch until
it is whitelisted, which can add a few hundred milliseconds):

| Implementation | Startup | Binary |
| :--- | ---: | ---: |
| `statusline` (Go) | **46 ms** | **2.5 MB** |
| legacy Bash script | 55 ms | — |
| legacy Perl script | 87 ms | — |

The floor is process creation, not parsing: the whole render is a few
milliseconds, the rest is the host spawning a new process per frame.

The balance adds one small cache read per frame (measured **50 µs**); the
network request runs in a detached worker and never contributes to the
foreground. The only visible cost is one process creation on the frame that
wins the refresh lock, once per TTL.

## Debugging

```bash
# inspect what the parser saw; stderr only, never pollutes the bar.
# Includes the billing state (amount, origin, age, staleness), never the key.
echo "$PAYLOAD" | statusline bar --debug

# force a width while iterating on layout
echo "$PAYLOAD" | statusline bar --width 157
```

In-production debugging without touching any agent configuration (swap the binary,
read the captured frame, restore) is documented in [AGENTS.md](AGENTS.md).

## Testing

```bash
go test ./...     # unit + golden + contract
make vet          # go vet + gofmt check
```

`testdata/golden` was generated by the historical shell implementation, so the
golden tests assert **byte-identical** output instead of self-consistency.
`scripts/fixture_paths.py` normalizes payloads (fixed directory, zeroed
timestamps, scrubbed e-mail addresses) so goldens never depend on the machine.

## License

MIT
