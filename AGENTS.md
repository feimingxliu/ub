# Repository Guidelines

## Project Structure & Module Organization

This repository is a Go CLI/TUI application with docs kept as the product spec. Product requirements live in `docs/requirements.md`, architecture in `docs/design.md`, and implementation slices in `docs/roadmap.md`; treat them as one linked spec. The Go module is rooted here: `cmd/ub/` is the CLI entrypoint, `cmd/gen-schema/` regenerates configuration schema, `internal/` contains application code, `schema/` contains generated JSON Schema, and `docs/` contains the maintained specs. Keep research material under ignored `.references/`.

## Build, Test, and Development Commands

Use the checked-in `Makefile` targets as thin wrappers around Go commands:

- `make build` or `go build -o ub ./cmd/ub` to build the CLI.
- `make test` or `go test ./...` to run the full suite.
- `make vet` or `go vet ./...` to catch common Go issues.
- `make lint` to run vet plus formatting checks.
- `make fmt` to format Go using `gofumpt` when installed, otherwise `gofmt`.
- `make schema` after changing `internal/config` types; commit `schema/config.schema.json`.
- `./ub --version`, `./ub run --help`, and `./ub config show` for CLI smoke checks.

In restricted environments where the default Go cache is not writable, set `GOCACHE=/tmp/ub-go-build`. Some tests use local `httptest` sockets; if sandboxing blocks loopback binds, rerun the same `go test` command with the required local-socket permission. Do not claim validation until the command has actually run.

## Coding Style & Naming Conventions

Use Go idioms: tabs via `gofmt`/`gofumpt`, short package names, exported identifiers only when needed, and explicit errors instead of panics outside process setup. Keep production packages under `internal/`; avoid public `pkg/` APIs unless the design changes. Name provider implementations after config types: `anthropic`, `openai`, `compat`, `ollama`, `fake`.

## Testing Guidelines

Place unit tests beside code as `*_test.go`. Prefer table-driven tests for config, tools, provider conversion, rollout storage, and permission decisions. Use the fake provider for deterministic agent-loop tests and VCR replay for real LLM HTTP interactions. Each implementation slice should leave `go test ./...` passing and include one real CLI smoke command when applicable.

## Commit & Pull Request Guidelines

Use Conventional Commits by default, for example `feat: add startup cleanup maintenance` or `fix: inject runtime workspace context`. Use `[I-NN] <summary>` only when the user explicitly asks for a specific roadmap iteration or the change is clearly implementing one named `docs/roadmap.md` item. Pull requests should state the roadmap item when applicable, summarize changes, list validation commands, and include screenshots or terminal output for TUI-visible changes.

## Documentation Sync

When changing behavior or scope, update the affected specification files together. Requirements define V1 scope, design defines interfaces and storage semantics, and roadmap defines delivery order and verification. If they disagree, resolve the discrepancy in docs before implementing code.

## Security & Configuration Tips

Never commit API keys, rollout databases, local config, or reference repositories. Configuration should read secrets from environment variables such as `${OPENAI_API_KEY}` and keep provider `base_url` or custom headers in user-local config files.
