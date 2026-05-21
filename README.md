<div align="center">

<img src="docs/img/banner.svg" alt="ub — Ulimited Blade — coding agent" width="600">

**A lean, hackable terminal coding agent — written in Go, local-first, every byte on disk.**

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go 1.25+](https://img.shields.io/badge/go-1.25%2B-00ADD8.svg)](https://go.dev/dl/)
[![Status: pre-release](https://img.shields.io/badge/status-pre--release-orange.svg)]()
[![中文](https://img.shields.io/badge/docs-中文-red.svg)](README.zh-CN.md)

</div>

---

## 👀 See it

<p align="center">
  <img src="docs/img/demo.gif" alt="ub demo" width="900">
</p>

## 🎯 What it's for

ub is a coding agent that lives entirely in your terminal. It speaks to your favorite LLM provider, runs tools in *this* directory, and persists every keystroke as a replayable event log. You can read the whole thing end-to-end — agent loop, provider adapters, TUI, MCP, LSP — and bend it to your workflow.

- 🧠 **Multi-provider.** Anthropic · OpenAI · OpenAI-compat (DeepSeek / Together / vLLM / LiteLLM) · Ollama · plus a script-driven Fake provider that runs CI offline.
- 🛠️ **Local tools.** Filesystem, search, shell, background jobs, LSP diagnostics, and any MCP server.
- 🛡️ **Permission-first.** Three execution modes (`work` / `plan` / `auto`), five-way approval modal, persistent allow-rules, hard-coded blocklist for `rm -rf /` and friends.
- 📜 **Every session replayable.** SQLite-backed append-only rollout log; inspect with `ub rollout show <id>`.
- 🪶 **Tiny surface area.** Single binary. No daemon. No telemetry. `~26k` lines of Go you can actually read.

## 🚀 Quick taste

**Grab a prebuilt binary** (Linux / macOS / Windows · amd64 / arm64):

```sh
# pick the archive that matches your platform
curl -LO https://github.com/feimingxliu/ub/releases/latest/download/ub_linux_amd64.tar.gz
tar -xzf ub_linux_amd64.tar.gz
install -m 0755 ub ~/.local/bin/ub
ub --version
```

> Prefer Go? `go install github.com/feimingxliu/ub/cmd/ub@latest` works too. Or `go build` from source.

**Point it at a provider**:

```yaml
# ~/.config/ub/config.yaml
default_provider: openai
default_model: gpt-4o-mini
providers:
  openai:
    type: openai
    api_key: ${OPENAI_API_KEY}
```

**Go**:

```sh
ub                                  # interactive TUI
ub run -p "summarize this repo"     # headless, CI-friendly
ub doctor --plain                   # check connectivity
```

No API key? Drop in the [Fake provider](docs/install.md#45-fake-provider-离线测试) and the whole agent loop runs offline.

## 🪞 How ub compares

|                          | **ub**                          | OpenCode                  | Claude Code                              | Codex CLI                |
|---|---|---|---|---|
| License                  | **MIT**                         | MIT                       | Proprietary                              | Apache-2.0               |
| Language                 | **Go** (~26k LoC)               | TypeScript                | closed-source                            | Rust                     |
| Terminal interface       | TUI + headless                  | TUI                       | TUI (+ IDE / Web / Desktop / Mobile)     | TUI                      |
| Provider count           | 5 (incl. Ollama + vLLM-compat)  | 75+ via AI SDK            | Anthropic + Bedrock / Vertex / Foundry   | OpenAI / ChatGPT only    |
| Session storage          | local SQLite                    | local                     | cloud-synced (account-bound)             | not publicly documented  |
| Replayable event log     | ✅ JSONL + `rollout show`       | `/undo` · `/redo`         | —                                        | not publicly documented  |
| MCP                      | stdio · http · sse              | ✅                        | ✅                                       | ✅                       |
| LSP integration          | ✅ pluggable                    | ✅                        | —                                        | —                        |
| Plan / read-only mode    | ✅                              | ✅                        | ✅                                       | ✅ (approval modes)      |
| Approval-by-LLM (auto)   | ✅ optional                     | —                         | —                                        | —                        |

<sub>Sources: [OpenCode docs](https://opencode.ai/docs/providers), [Claude Code overview](https://code.claude.com/docs/en/overview) and [enterprise deployment](https://code.claude.com/docs/en/third-party-integrations), [Codex CLI repo](https://github.com/openai/codex). Verified Nov 2025. Things move fast — if a row is wrong, open an issue.</sub>

Pick the right tool for the job. ub is best when you want **a complete event log you can `grep` through**, **a Go codebase small enough to read in an afternoon**, and **the freedom to swap providers or rip out subsystems**. It is not (yet) the polished product Claude Code or OpenCode are; if you need agent skills, IDE integration, or the world's biggest provider list, look there first.

## 🧱 Inside

```
        TUI / headless ── agent loop ── provider ── LLM
                              │
                ┌─────────────┼─────────────┐
                ▼             ▼             ▼
            tool registry  permission   rollout log
            (fs / shell /  (modes +     (SQLite,
            search / lsp / 5 decisions  append-only)
            mcp / jobs)    + blocklist)
```

Follow one keystroke from `internal/tui/model.go` through `internal/agent/`, `internal/tool/`, `internal/permission/`, into `internal/rollout/` and back. That's the whole control flow.

See [`docs/design.md`](docs/design.md) for the long version.

## 📚 Documentation

| | |
|---|---|
| [`docs/install.md`](docs/install.md) | Install, configure, upgrade, uninstall |
| [`docs/usage.md`](docs/usage.md) | TUI keymap · slash commands · execution modes · permission flow · workflows |
| [`docs/design.md`](docs/design.md) | Architecture, module boundaries, data flow |
| [`docs/roadmap.md`](docs/roadmap.md) | Six-sprint iteration plan |
| [`README.zh-CN.md`](README.zh-CN.md) | 中文文档 |
| [`AGENTS.md`](AGENTS.md) | Repository conventions (commit style, testing, etc.) |

## 🛣️ Status

V1 scope ([`docs/roadmap.md`](docs/roadmap.md)) is feature-complete. The codebase is approaching its first tagged release. Expect rough edges; expect API churn before `v1.0.0`.

If something looks wrong, [open an issue](https://github.com/feimingxliu/ub/issues). PRs welcome — see [`AGENTS.md`](AGENTS.md) for conventions.

## 🙏 Acknowledgments

ub stands on the shoulders of:

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) and the rest of the Charm stack for the TUI runtime
- [Cobra](https://github.com/spf13/cobra) for the CLI surface
- [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) for the pure-Go SQLite driver
- [aymanbagabas/go-udiff](https://github.com/aymanbagabas/go-udiff) for fast unified diffs
- And the agentic-CLI lineage: [Claude Code](https://code.claude.com), [OpenCode](https://opencode.ai), [Codex CLI](https://github.com/openai/codex) — much of ub's shape is influenced by what these did well

## 📄 License

[MIT](LICENSE). Fork it. Ship it. Rip out the parts you don't like.

<div align="center">

—

<sub>Built in the open · star if it sparks ideas · `feimingxliu/ub`</sub>

</div>
