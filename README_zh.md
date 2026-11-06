# statusline — 一个二进制，搞定所有 AI Agent 的状态栏与终端标题

[![CI](https://github.com/hughcube/statusline/actions/workflows/ci.yml/badge.svg)](https://github.com/hughcube/statusline/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/hughcube/statusline)](https://github.com/hughcube/statusline/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

[English](README.md) | 中文

**一份实现，所有 Agent 共用。** `statusline` 用一个 Go 静态二进制，替代过去按 Agent
分别维护的 Bash / Perl 脚本，统一渲染终端底部的两行状态栏与窗口标题。同一份输出可原样跑在
**CodeBuddy / WorkBuddy**、**Google Antigravity**、**Claude Code**，以及任何能把 JSON
payload 通过 stdin 喂进来的新 Agent 上。

---

## 目录

- [为什么需要 statusline](#为什么需要-statusline)
- [支持的 Agent](#支持的-agent)
- [功能一览](#功能一览)
- [安装](#安装)
- [配置](#配置)
- [DeepSeek 余额](#deepseek-余额)
- [宽度契约](#宽度契约)
- [性能](#性能)
- [调试](#调试)
- [测试](#测试)
- [许可证](#许可证)

---

## 为什么需要 statusline

| 维度 | 每个 Agent 一份脚本 | `statusline` |
| :--- | :--- | :--- |
| **多 Agent** | 一个 CLI 一份 Bash，另一个一份 Perl，越走越散 | 一个二进制，适配器藏在接口后面 |
| **多平台** | 依赖 bash / perl 存在（macOS 一直在砍运行时） | 静态二进制，零运行时依赖 |
| **宽度** | 每份脚本各写一套终端探测 | 一套契约，列预算 + 字节预算 |
| **测试** | 只能写 shell 断言，CI 里难跑 | `go test`：单元 + golden + 契约 |
| **调试** | 打印大法 | `--debug` 把解析结果 dump 到 stderr |

渲染层刻意保持"笨"：只接收归一化字段，输出两行字符串。所有 Agent 相关的差异都在
`internal/agent` 里，新增一种 CLI = 加一个文件 + 注册一行。

## 支持的 Agent

| Agent | 状态栏 | 终端标题 | 说明 |
| :--- | :---: | :---: | :--- |
| CodeBuddy / WorkBuddy | 是 | — | 窗口占用取 `used_percentage`，按字节预算裁切 |
| Google Antigravity | 是 | 是 | 配额组 `P` / `G` / `G4`，状态图标带动效 |
| Claude Code | 是 | — | `rate_limits`（含重置时间）+ DeepSeek 余额 |
| 其它 | 尽力而为 | — | generic 适配器 |

## 功能一览

- **多 Agent 统一状态栏**：Claude Code、CodeBuddy / WorkBuddy、Google Antigravity 共用一份二进制。
- **DeepSeek 余额**：直连官方 `/user/balance`，在 line 2 显示 `DeepSeek ¥10.26`；余额不足 `!`，缓存陈旧 `?`。
- **额度与重置时间**：5 小时 / 7 天窗口使用率，支持 `resets_at` 本地时间。
- **Token 与成本**：In / Out / Cache / CacheWrite、上下文占用、会话成本、耗时。
- **Git 上下文**：分支、脏文件数、ahead/behind、增删行数、worktree。
- **宽度契约**：实时宽度文件、payload 宽度、`STATUSLINE_WIDTH`、`COLUMNS` 逐级回退，CodeBuddy 额外字节预算。
- **前台零阻塞**：Git 和余额都由后台 worker 刷新，状态栏只读缓存。
- **单二进制、零运行时依赖**：不需要 bash / perl / node，Windows / macOS / Linux 同一份配置。

## 安装

从 [releases](https://github.com/hughcube/statusline/releases) 下载对应平台的文件，
放到 `~/.agents/statusline`。Windows 下文件名是 `statusline.exe`，但命令写
`~/.agents/statusline` 依然能被解析（走 `PATHEXT`），因此三个平台可以用完全
相同的配置。

从源码构建：

```bash
make build      # 本机二进制
make cross      # 六个平台产物，输出到 dist/
```

## 配置

CodeBuddy（`~/.codebuddy/settings.json`）：

```json
{
  "statusLine": {
    "type": "command",
    "command": "~/.agents/statusline bar --agent codebuddy",
    "padding": 0
  }
}
```

Antigravity：

```json
{
  "statusLine": { "type": "command", "command": "~/.agents/statusline bar --agent antigravity" },
  "title":      { "type": "command", "command": "~/.agents/statusline title --agent antigravity" }
}
```

Claude Code：

```json
{
  "statusLine": { "type": "command", "command": "~/.agents/statusline bar --agent claude" }
}
```

## DeepSeek 余额

当 Claude Code 使用 **官方** DeepSeek 端点（`ANTHROPIC_BASE_URL=https://api.deepseek.com/...`）时，
line 2 会在额度段前插入当前会话 key 的账户余额：

```text
DeepSeek ¥10.26 | 23%(18:30)/41%(08 09:00) | deepseek-flash 1.0M | v2.1.0
```

余额来自官方 `GET https://api.deepseek.com/user/balance`，只读取 `total_balance`；
官方没有公开的消费/用量查询 API，因此状态栏不显示日/月消费。

**显示规则**

| 状态 | 显示 |
| :--- | :--- |
| 正常 | `DeepSeek ¥10.26` |
| 余额不足以调用 API | `DeepSeek ¥0.00!` |
| 缓存超过 2×TTL 未刷新或刷新失败 | `DeepSeek ¥10.26?` |
| 超过 `max(2h, 3×TTL)` | 隐藏余额段 |
| 窄屏 | 先去掉 `DeepSeek`，保留 `¥10.26`；再窄则随账户上下文隐藏 |

**识别与凭证**

- 只接受 `https://api.deepseek.com`（hostname 精确匹配）；第三方中转的 token 绝不会发给官方。
- 使用当前 Claude Code 会话环境里的 `ANTHROPIC_AUTH_TOKEN`（其次 `ANTHROPIC_API_KEY`）；
  不同 key 对应不同缓存文件，切换 key 不会串号。
- 账户段（`HAIKIT_ACCOUNT` 或 payload `email`）与余额段相互独立；
  DeepSeek 余额接口不返回账户名，所以 Claude Code 下通常只显示 `DeepSeek ¥10.26`。

**缓存与性能**

- 缓存：`~/.cache/statusline/billing/deepseek-<sha256:16>.json`；
  文件名是 `sha256("statusline:billing:deepseek:" + key)` 的前 16 位，
  不含 key 原文、片段、邮箱或 base URL。
- 缓存保存**完整的 `/user/balance` 响应 JSON** 加本地元数据，而不是只存当前显示的字段；
  多币种、未来新增字段都不会丢。文件里没有 key、请求头或账户信息。
- 前台每帧只读一个小 JSON 文件（实测约 50 µs），**不做网络请求**；过期时拉起 detached worker
  （请求超时 3 秒），本帧立即返回旧值。
- 默认刷新间隔 60 秒；刷新失败退避 60 秒；`?` 在 2×TTL 后出现；`max(2h, 3×TTL)` 后隐藏。

缓存示例：

```json
{
  "version": 1,
  "fetched_at": 1762473600,
  "last_attempt": 1762473600,
  "status": "ok",
  "response": {
    "is_available": true,
    "balance_infos": [
      {
        "currency": "CNY",
        "total_balance": "10.26",
        "granted_balance": "0.00",
        "topped_up_balance": "10.26"
      }
    ]
  }
}
```

**环境变量**

| 变量 | 默认 | 说明 |
| :--- | ---: | :--- |
| `STATUSLINE_BILLING_TTL` | `60` | 成功刷新间隔（秒），最小 30，非法值回退 60 |
| `STATUSLINE_BILLING` | 开启 | `off` / `0` / `false` / `no` 完全关闭 |
| `STATUSLINE_TESTING` | — | `1` 时禁用账单，保证测试确定性 |

**范围**

当前仅 Claude Code 接入；`agent.Billable` 是扩展点，CodeBuddy / Antigravity 将来加一个方法即可。

## 宽度契约

状态栏**从不探测终端**。宽度按以下顺序取值：

1. `STATUSLINE_WIDTH_FILE` —— 包装层每秒刷新的实时文件（resize 自动跟随）
2. payload 里的 `terminal_width` / `columns`（Antigravity 每帧上报）
3. `STATUSLINE_WIDTH` —— 包装层启动快照（显式 `--width` 优先于它）
4. `COLUMNS` —— 宿主每次运行时注入；Claude Code 用 TUI 当前宽度设置它，
   因为它的 payload 不带终端宽度
5. `120` —— 兜底常量

诊断构建里 `width_src` 会标明实际生效的那一级：`live` / `json` / `env` /
`columns` / `const`。

容器宽度：codebuddy `W-1`，antigravity `W`，其余 `W-4`。

CodeBuddy 额外受**字节预算**约束：它的渲染器按字节折行，而终端按列排版，中文字符
占 2 列却是 3 字节——只按列算必然被挤到下一行。

## 性能

Windows 11 实测，15 次取样，有 payload 输入、输出两行（杀软已关闭；若杀软常驻，新下载的二进制在加入白名单前每次启动都会被扫描，可能多出数百毫秒）：

| 实现 | 启动耗时 | 体积 |
| :--- | ---: | ---: |
| `statusline`（Go） | **46 ms** | **2.5 MB** |
| 旧 Bash 脚本 | 55 ms | — |
| 旧 Perl 脚本 | 87 ms | — |

下限由"每帧创建一个进程"决定，而不是解析开销：整次渲染只有几毫秒。

余额段每帧只多一次小文件读取（实测 **50 µs**）；网络请求在 detached worker 里执行，
不计入前台耗时。唯一可见开销是抢到刷新锁的那一帧多一次进程创建，每个 TTL 才发生一次。

## 调试

```bash
# 看解析结果；只写 stderr，不会污染状态栏。
# 同时输出 billing 状态（金额、来源、时间、是否陈旧），绝不含 key。
echo "$PAYLOAD" | statusline bar --debug

# 调排版时强制宽度
echo "$PAYLOAD" | statusline bar --width 157
```

线上排查（不改任何 agent 配置：换二进制 → 读抓帧 → 还原）见 [AGENTS.md](AGENTS.md)。

## 测试

```bash
go test ./...     # 单元 + golden + 契约
make vet          # go vet + gofmt 检查
```

`testdata/golden` 由旧 shell 实现生成，因此 golden 用例断言的是**与历史行为逐字节
一致**，而不是"当前实现自己跟自己比"。`scripts/fixture_paths.py` 负责把 payload
中的目录、时间戳和邮箱规范化，保证 golden 与机器无关。

## 许可证

MIT
