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
- [安装](#安装)
- [配置](#配置)
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
| Claude Code | 是 | — | 只有 `rate_limits` |
| 其它 | 尽力而为 | — | generic 适配器 |

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

## 宽度契约

状态栏**从不探测终端**。宽度按以下顺序取值：

1. `STATUSLINE_WIDTH_FILE` —— 包装层每秒刷新的实时文件（resize 自动跟随）
2. payload 里的 `terminal_width` / `columns`（Antigravity 每帧上报）
3. `STATUSLINE_WIDTH` —— 包装层启动快照
4. `120` —— 兜底常量

容器宽度：codebuddy `W-1`，antigravity `W`，其余 `W-4`。

CodeBuddy 额外受**字节预算**约束：它的渲染器按字节折行，而终端按列排版，中文字符
占 2 列却是 3 字节——只按列算必然被挤到下一行。

## 性能

Windows 11 + 杀软常驻环境实测（有 payload 输入，输出两行）：

| 实现 | 启动耗时 | 体积 |
| :--- | ---: | ---: |
| `statusline`（Go） | **70 ms** | **2.7 MB** |
| 旧 Bash 脚本 | 74 ms | — |
| 旧 Perl 脚本 | 109 ms | — |

下限由"每帧创建一个进程"决定，而不是解析开销：整次渲染只有几毫秒。

## 调试

```bash
# 看解析结果；只写 stderr，不会污染状态栏
echo "$PAYLOAD" | statusline bar --debug

# 调排版时强制宽度
echo "$PAYLOAD" | statusline bar --width 157
```

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
