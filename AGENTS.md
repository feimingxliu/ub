# Repository Guidelines

## Project Structure & Module Organization

This repository is currently documentation-first. Keep product requirements in `docs/requirements.md`, architecture and package design in `docs/design.md`, and implementation slices in `docs/roadmap.md`. The planned Go layout is `cmd/ub/` for the CLI entrypoint, `internal/` for application code, `schema/` for generated JSON Schema, and `docs/` for project documentation. Keep reference checkouts and research material under `.references/`; it is ignored and must not be treated as source.

## Build, Test, and Development Commands

There is no `go.mod`, `Makefile`, or runnable binary yet. After the I-01 scaffold in `docs/roadmap.md`, use:

- `go build ./...` to compile all packages.
- `go test ./...` to run the full test suite.
- `go vet ./...` to catch common Go issues.
- `gofumpt -w .` to format Go code once `gofumpt` is introduced.
- `./ub --version` and `./ub run --help` as CLI smoke checks after building.

If a `Makefile` or `justfile` is added, keep `build`, `test`, and `lint` targets as thin wrappers around these commands.

## Coding Style & Naming Conventions

Use Go idioms: tabs via `gofmt`/`gofumpt`, short package names, exported identifiers only when needed, and explicit errors instead of panics outside process setup. Keep production packages under `internal/`; avoid public `pkg/` APIs unless the design changes. Name provider implementations after their config types, such as `anthropic`, `openai`, `compat`, `ollama`, and `fake`.

## Testing Guidelines

Place unit tests beside code as `*_test.go`. Prefer table-driven tests for config loading, tool behavior, provider conversion, rollout storage, and permission decisions. The roadmap expects offline-first development: use the fake provider for deterministic agent-loop tests and VCR replay for real LLM HTTP interactions. Every implementation slice should leave `go test ./...` passing and include one CLI smoke command when applicable.

## Commit & Pull Request Guidelines

The current history uses Conventional Commits, for example `chore: init repo`. Continue with concise subjects such as `feat(cli): add cobra root command`, `test(config): cover env expansion`, or `docs: update roadmap`. Pull requests should state the roadmap item, summarize behavior changes, list validation commands, and include screenshots or terminal output for TUI-visible changes.

## Security & Configuration Tips

Never commit API keys, rollout databases, local config, or reference repositories. Configuration should read secrets from environment variables such as `${OPENAI_API_KEY}` and keep provider `base_url` or custom headers in user-local config files.
