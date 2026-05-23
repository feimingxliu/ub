# Contributing to ub

Thanks for taking the time to improve ub. This project treats the docs as the
product spec, so code changes and spec changes should stay aligned.

## Project Layout

- `cmd/ub/`: CLI entrypoint.
- `cmd/gen-schema/`: regenerates `schema/config.schema.json`.
- `internal/`: application packages.
- `schema/`: generated JSON Schema.
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
make schema     # regenerate schema/config.schema.json after config changes
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
  `compat`, `ollama`, `fake`.
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

## Security and Configuration

Never commit API keys, rollout databases, local config, or reference
repositories. Configuration should read secrets from environment variables such
as `${OPENAI_API_KEY}` and keep provider `base_url` or custom headers in
user-local config files.
