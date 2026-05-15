# Repository Guidelines

## Project Structure & Module Organization

This repository is documentation-first. Product requirements live in `docs/requirements.md`, architecture in `docs/design.md`, and implementation slices in `docs/roadmap.md`; treat them as one linked spec. The planned Go layout is `cmd/ub/` for the CLI entrypoint, `internal/` for application code, `schema/` for generated JSON Schema, and `docs/` for documentation. Keep research material under ignored `.references/`.

## Build, Test, and Development Commands

There is no `go.mod`, `Makefile`, or runnable binary yet. After I-01 in `docs/roadmap.md`, use:

- `go build ./...` to compile packages.
- `go test ./...` to run tests.
- `go vet ./...` to catch common Go issues.
- `gofumpt -w .` to format Go once introduced.
- `./ub --version` and `./ub run --help` for CLI smoke checks.

If a `Makefile` or `justfile` is added, keep `build`, `test`, and `lint` as thin wrappers. Do not claim validation until the scaffold exists and the command has run.

## Coding Style & Naming Conventions

Use Go idioms: tabs via `gofmt`/`gofumpt`, short package names, exported identifiers only when needed, and explicit errors instead of panics outside process setup. Keep production packages under `internal/`; avoid public `pkg/` APIs unless the design changes. Name provider implementations after config types: `anthropic`, `openai`, `compat`, `ollama`, `fake`.

## Testing Guidelines

Place unit tests beside code as `*_test.go`. Prefer table-driven tests for config, tools, provider conversion, rollout storage, and permission decisions. Use the fake provider for deterministic agent-loop tests and VCR replay for real LLM HTTP interactions. Each implementation slice should leave `go test ./...` passing and include one real CLI smoke command when applicable.

## Commit & Pull Request Guidelines

The initial history uses Conventional Commits, for example `chore: init repo`, but roadmap work should follow `docs/roadmap.md`: one commit per iteration with subject `[I-NN] <summary>`, such as `[I-01] add cobra root command`. Pull requests should state the roadmap item, summarize changes, list validation commands, and include screenshots or terminal output for TUI-visible changes.

## Documentation Sync

When changing behavior or scope, update the affected specification files together. Requirements define V1 scope, design defines interfaces and storage semantics, and roadmap defines delivery order and verification. If they disagree, resolve the discrepancy in docs before implementing code.

## Security & Configuration Tips

Never commit API keys, rollout databases, local config, or reference repositories. Configuration should read secrets from environment variables such as `${OPENAI_API_KEY}` and keep provider `base_url` or custom headers in user-local config files.
