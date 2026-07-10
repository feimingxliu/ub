# Contributing to ub

Thanks for taking the time to improve ub. This project treats the docs as the
product spec, so code changes and spec changes should stay aligned.

## Project Layout

- `cmd/ub/`: CLI entrypoint.
- `tools/gen-schema/`: regenerates `api/config.schema.json`.
- `internal/agent/`: headless provider/tool agent loop.
- `internal/command/`: cobra CLI command definitions.
- `internal/tui/`: bubbletea TUI views.
- `internal/config/`: config loading, merging, and redaction.
- `internal/message/`: provider-neutral message model.
- `internal/mode/`: execution mode (work/plan/auto/full-access) and gates.
- `internal/reasoning/`: provider-neutral reasoning controls.
- `internal/provider/`: provider interface, registry, and adapters (anthropic, openai, compat, fake).
- `internal/tokenizer/`: token estimation.
- `internal/context/`: context window resolver and store.
- `internal/modelinfo/`: model metadata.
- `internal/vcr/`: HTTP VCR for LLM integration tests.
- `internal/permission/`: execution-mode-aware tool approval.
- `internal/approval/`: secondary approval-agent interface.
- `internal/hook/`: session event hooks.
- `internal/logx/`: slog-based logging with rotation.
- `internal/maintenance/`: startup cleanup and maintenance.
- `internal/store/`: SQLite-backed session persistence and migrations.
- `internal/rollout/`: rollout event log reader/writer over the shared store DB.
- `internal/workspace/`: workspace paths, workspace memory, file history, and tool output spillover.
- `internal/tool/`: tool registry plus built-in tools (fs, goal, job, lsp, mcp, memory, plan, procgroup, search, shell, task, todo, web).
- `internal/lsp/`: LSP protocol client.
- `internal/mcp/`: MCP protocol client.
- `api/`: generated JSON Schema.
- `docs/requirements.md`, `docs/design.md`, `docs/roadmap.md`, and
  `docs/roadmap-v2.md`: maintained product and implementation specs.
- `.references/`: ignored local research material.

## Development Commands

Use the checked-in `Makefile` targets as thin wrappers around Go commands:

```sh
make build      # or: go build -o ub ./cmd/ub
make test       # or: go test ./...
make vet        # or: go vet ./...
make lint       # vet + strict gofumpt check
make fmt        # format Go with gofumpt
make check      # CI-equivalent gate: lint + race tests + build
make schema     # regenerate api/config.schema.json after config changes
```

Useful smoke checks:

```sh
./ub --version
./ub run --help
./ub config show
```

In restricted environments where the default Go cache is not writable, set:

```sh
GOCACHE=/tmp/ub-go-build
```

Some tests use local `httptest` sockets. Do not claim validation until the test
command has actually run in an environment that permits the required loopback
binds.

## Formatting and Tests

- Use Go idioms: `gofmt`/`gofumpt` tabs, short package names, explicit errors
  instead of panics outside process setup.
- Keep production packages under `internal/`; avoid public `pkg/` APIs unless
  the design changes.
- Name provider implementations after config types: `anthropic`, `openai`,
  `compat`, `fake`.
- Put unit tests beside code as `*_test.go`.
- Prefer table-driven tests for config, tools, provider conversion, rollout
  storage, and permission decisions.
- Use the fake provider for deterministic agent-loop tests and VCR replay for
  real LLM HTTP interactions.

Run once per clone:

```sh
make install-hooks
```

The pre-commit hook runs `make lint`; the pre-push hook runs `make check`.
Bypass with `--no-verify` only when you genuinely need to.

## Documentation Sync

When changing behavior or scope, update the affected specification files
together. Requirements define V1 scope, design defines interfaces and storage
semantics, and roadmap files define delivery order and future work. If they
disagree, resolve the discrepancy in docs before implementing code.

## Commit Style

Use Conventional Commits by default:

```text
feat: add startup cleanup maintenance
fix: inject runtime workspace context
docs: add release verification steps
test: fuzz permission blacklist normalization
```

Use `[I-NN] <summary>` only when a maintainer explicitly asks for a specific
roadmap iteration or the change clearly implements a named `docs/roadmap.md`
item.

## Pull Requests

PRs should include:

- The roadmap/spec item when applicable.
- A concise summary of the behavior change.
- Validation commands that actually ran.
- Screenshots or terminal output for TUI-visible changes.
- Notes for docs updates or breaking changes.

Before opening a PR, run:

```sh
make check
```

## Release Flow

Maintainers cut a release with a single command from a clean `main`:

```sh
make release VERSION=0.2.7
```

This regenerates `CHANGELOG.md` to include the new version section, commits
it, creates an annotated `vX.Y.Z` tag, and pushes commit + tag together. The
release workflow then extracts the matching section from `CHANGELOG.md` as
GoReleaser release notes — no changelog work happens in CI.

Preflight checks the target enforces: working tree clean, on `main`, tag
does not already exist, and `make check` passes.

## Security and Configuration

Never commit API keys, rollout databases, local config, or reference
repositories. Configuration should read secrets from environment variables such
as `${OPENAI_API_KEY}` and keep provider `base_url` or custom headers in
user-local config files.
