<div align="center">

<img src="docs/img/banner.svg" alt="ub — Ulimited Blade — coding agent" width="600">

**一个轻量、易改、住在终端里的编程 Agent — Go 写就，本地优先,每一字节都在你磁盘上。**

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go 1.25+](https://img.shields.io/badge/go-1.25%2B-00ADD8.svg)](https://go.dev/dl/)
[![Status: release](https://img.shields.io/badge/status-release-brightgreen.svg)]()
[![English](https://img.shields.io/badge/docs-English-blue.svg)](README.md)

</div>

---

## 👀 看一眼

<p align="center">
  <img src="docs/img/demo.gif" alt="ub demo" width="900">
</p>

## 🎯 这是干嘛的

ub 是一个完全活在终端里的编程 Agent。它接你喜欢的 LLM provider,在 *当前* 工作区里跑工具,把每一次按键持久化成可回放的事件流。整个项目 — agent loop、provider 适配、TUI、MCP、LSP — 都能从头读到尾,也能按你的工作流改造。

- 🧠 **多 Provider。** Anthropic · OpenAI · OpenAI 兼容（DeepSeek / Together / vLLM / LiteLLM / Ollama `/v1`），外加脚本驱动的 Fake provider 让 CI 完全离线。
- 🛠️ **本地工具。** 文件系统 / 搜索 / Shell / 后台任务 / LSP diagnostics / 任意 MCP server。
- 🛡️ **权限优先。** 三种执行模式（`work` / `plan` / `auto`）、5 种审批 Decision、可持久化的 allow 规则、硬编码黑名单拦截 `rm -rf /` 类危险命令。
- 📜 **每次会话都可回放。** SQLite 上的 append-only rollout 日志,`ub rollout show <id>` 一行查看。
- 🪶 **小到能读完。** 单二进制,无 daemon,无遥测,~26k 行 Go,真能从头看完。

## 🚀 30 秒上手

**直接下载二进制**（Linux / macOS / Windows · amd64 / arm64）:

```sh
# 替换为你的平台:linux_amd64 / darwin_arm64 / linux_arm64 等
curl -LO https://github.com/feimingxliu/ub/releases/latest/download/ub_linux_amd64.tar.gz
tar -xzf ub_linux_amd64.tar.gz
install -m 0755 ub ~/.local/bin/ub
ub --version
```

> 也能用 `go install github.com/feimingxliu/ub/cmd/ub@latest`,或者 `go build` 源码构建。详见 [`docs/install.md`](docs/install.md)。

**配一个 provider**:

```yaml
# ~/.config/ub/config.yaml
default_provider: openai
default_model: gpt-4o-mini
providers:
  openai:
    type: openai
    api_key: ${OPENAI_API_KEY}
```

**跑起来**:

```sh
ub                                  # 交互式 TUI
ub run -p "总结一下这个仓库"          # 无头模式,CI 友好
ub doctor --plain                   # 体检
```

## 🧱 内部一览

```
        TUI / 无头 ── agent loop ── provider ── LLM
                          │
            ┌─────────────┼─────────────┐
            ▼             ▼             ▼
        工具注册表      权限系统       rollout 日志
        (fs / shell /  (3 种模式 +   (SQLite 追加,
        search / lsp / 5 种 decision   不可篡改)
        mcp / jobs)    + 黑名单)
```

从 `internal/tui/model.go` 跟一次按键开始,经过 `internal/agent/`、`internal/tool/`、`internal/permission/`,最后落到 `internal/rollout/`——整个控制流就这一条线。

完整设计参见 [`docs/design.md`](docs/design.md)。

## 📚 文档

| | |
|---|---|
| [`docs/install.md`](docs/install.md) | 安装、配置、升级、卸载 |
| [`docs/usage.md`](docs/usage.md) | TUI 键位 · Slash 命令 · 执行模式 · 权限审批 · 常见工作流 |
| [`docs/design.md`](docs/design.md) | 架构、模块边界、数据流 |
| [`docs/roadmap.md`](docs/roadmap.md) | V1 路线图：6 个 Sprint 35 个迭代（历史档案）|
| [`docs/roadmap-v2.md`](docs/roadmap-v2.md) | V2 路线图：功能、架构、生态演进 |
| [`README.md`](README.md) | English version |
| [`AGENTS.md`](AGENTS.md) | 仓库协作规范（commit 风格、测试要求） |

## 🛣️ 现状

V1 范围（[`docs/roadmap.md`](docs/roadmap.md)）已经 feature-complete,且已有 tagged release 可用。当前 release 仍是 `v0.x`,预期还会有粗糙的边角和 `v1.0.0` 前的 API 变动。

有问题请[开 issue](https://github.com/feimingxliu/ub/issues)。欢迎 PR,贡献约定见 [`AGENTS.md`](AGENTS.md)。

## 🙏 致谢

ub 站在以下项目的肩上:

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) 以及整个 Charm 系列,提供了 TUI 运行时
- [Cobra](https://github.com/spf13/cobra),CLI 骨架
- [modernc.org/sqlite](https://gitlab.com/cznic/sqlite),纯 Go SQLite 驱动
- [aymanbagabas/go-udiff](https://github.com/aymanbagabas/go-udiff),快速 unified diff
- 以及前辈们: [Claude Code](https://code.claude.com)、[OpenCode](https://opencode.ai)、[Codex CLI](https://github.com/openai/codex) — ub 的很多形态都受它们影响

## 📄 许可证

[MIT](LICENSE)。Fork 它,发布它,把不喜欢的部分删掉。

<div align="center">

—

<sub>Built in the open · 觉得有意思就 star · `feimingxliu/ub`</sub>

</div>
