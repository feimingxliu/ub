# ub

ub is a terminal coding agent implemented as a Go CLI/TUI. It keeps the core
loop local-first: provider adapters stream model output, tools run in the
current workspace, permission policy gates side effects, and every session is
persisted as a replayable rollout log.

ub 是一个用 Go 编写的终端编码 Agent，包含 CLI 和 TUI 两种入口。它的核心链路是本地优先的：Provider 适配层负责模型流式输出，工具在当前工作区执行，权限策略拦截有副作用的操作，并且每个会话都会写入可重放的 rollout 事件日志。

## Features

- CLI commands for headless `run`, direct `chat`, session management, `doctor`,
  and rollout inspection.
- Bubble Tea TUI with streaming output, slash commands, session switching,
  permission modals, diff previews, local `!cmd`, and `@file` insertion.
- Provider support for fake, Anthropic, OpenAI, OpenAI-compatible endpoints,
  and Ollama.
- Local tools for filesystem access, grep/search, shell commands, background
  jobs, MCP tools, and LSP diagnostics/references.
- Context management with token estimation, automatic summary, manual
  `/compact`, and tool-result spillover files.
- SQLite session storage with startup cleanup, log rotation, and JSONL rollout
  inspection via `ub rollout show --json`.

## Quick Start

Build from source:

```sh
go build -o ub ./cmd/ub
./ub --version
```

Create a minimal local config:

```yaml
# .ub/config.yaml
default_provider: fake
default_model: fake/demo
providers:
  fake:
    type: fake
    script:
      - type: text_delta
        text: hello from ub
      - type: done
```

Run the CLI:

```sh
./ub chat "hello"
./ub run -p "list the markdown files in this repository"
./ub sessions ls
./ub rollout show <session-id>
```

Run the TUI:

```sh
./ub
./ub --resume
./ub --resume=<session-id>
```

## Configuration

ub loads YAML configuration in this order:

1. Built-in defaults.
2. `~/.config/ub/config.yaml`.
3. The nearest workspace `.ub/config.yaml`.
4. Environment variable substitution such as `${OPENAI_API_KEY}`.
5. A selected profile from `--profile`, `--dev`, or `UB_PROFILE`.
6. Runtime CLI overrides such as `--mode`.

Useful commands:

```sh
./ub config show
./ub config path
./ub doctor --plain
```

By default, session data lives in `$XDG_DATA_HOME/ub/ub.db` or
`~/.local/share/ub/ub.db`, state files live in `$XDG_STATE_HOME/ub` or
`~/.local/state/ub`, and global permission rules live in
`~/.config/ub/permissions.yaml`.

## Development

The product specification is maintained in `docs/requirements.md`,
`docs/design.md`, and `docs/roadmap.md`.

```sh
make build
make test
make vet
make lint
make schema
```

If the default Go build cache is not writable, use:

```sh
GOCACHE=/tmp/ub-go-build go test ./...
```

Some tests use local `httptest` sockets. In restricted sandboxes, rerun the same
test command with loopback permission rather than treating the failure as a
product regression.

## Release

Releases are built by GoReleaser from tags matching `v*.*.*`. The release
workflow publishes multi-platform archives and `checksums.txt`.

```sh
git tag v0.1.0
git push origin v0.1.0
```

See `docs/install.md` for installation and verification details.

