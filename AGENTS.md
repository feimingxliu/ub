# Agent Collaboration Guide

This file is for AI coding agents working in this repository. Human contributor
guidance lives in [`CONTRIBUTING.md`](CONTRIBUTING.md).

## Ground Rules

- Treat `docs/requirements.md`, `docs/design.md`, `docs/roadmap.md`, and
  `docs/roadmap-v2.md` as one linked product spec.
- Read the relevant code before proposing edits. Prefer existing package
  boundaries and local helper APIs over new abstractions.
- Keep changes scoped to the user request. Do not refactor unrelated modules or
  churn generated files unless the change requires it.
- Never commit API keys, rollout databases, local config, or material under
  ignored `.references/`.
- Do not revert user changes. If the tree is dirty, inspect the diff and work
  around unrelated edits.

## Validation

- Use `make test`, `make lint`, `make build`, and `make check` when the change
  warrants them.
- Use `GOCACHE=/tmp/ub-go-build` when the default Go cache is not writable.
- Some tests use local `httptest` sockets. If sandboxing blocks loopback binds,
  rerun the same test with the required local-socket permission before claiming
  validation.
- Run `make schema` after changing `internal/config` types and commit
  `schema/config.schema.json`.
- Use smoke commands such as `./ub --version`, `./ub run --help`, and
  `./ub config show` for CLI-visible changes.
- Platform workflow runs tests on Windows. Keep path assertions portable with
  `filepath.Join`, `filepath.ToSlash`, or existing repo path helpers instead of
  hard-coding `/` separators.

## Documentation Sync

- Update docs together with behavior changes. Requirements define scope, design
  defines interfaces and storage semantics, and roadmap files track delivery.
- If docs and code disagree, resolve the discrepancy rather than treating the
  code as implicitly authoritative.

## Commits

- Use Conventional Commits unless the user asks for a roadmap iteration prefix.
- Keep commits narrow. If a request covers several roadmap tasks, stage and
  commit each task separately.

## Releases

- Prefer `make release VERSION=x.y.z` for releases so `CHANGELOG.md`, the
  release commit, and the annotated tag stay in sync.
- Before pushing a manual `vX.Y.Z` tag, run
  `make release-notes VERSION=x.y.z` and ensure it succeeds.
- If a tag-triggered Release or Platform workflow fails after a tag push,
  delete both the local tag and `origin` tag, fix the root cause, then create
  and push a fresh annotated tag from the fixed commit.
