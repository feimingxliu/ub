# Installing ub

This document covers source builds, release archives, local configuration, and
basic verification for ub.

## Requirements

- Go matching `go.mod`.
- Optional but recommended: `rg`, `gopls`, `typescript-language-server`, and
  `npx` for the full tool surface.
- Provider credentials or a local model endpoint for real model use.

## Install From Source

```sh
git clone https://github.com/feimingxliu/ub.git
cd ub
go build -o ub ./cmd/ub
./ub --version
```

During development, the Makefile wraps the same commands:

```sh
make build
make test
make vet
make lint
```

If the default Go build cache is not writable:

```sh
GOCACHE=/tmp/ub-go-build go test ./...
```

## Install From A Release Archive

Download the archive for your platform from the GitHub release, then unpack it:

```sh
tar -xzf ub_v0.1.0_linux_amd64.tar.gz
install -m 0755 ub ~/.local/bin/ub
ub --version
```

Windows release archives use `.zip`:

```powershell
Expand-Archive .\ub_v0.1.0_windows_amd64.zip
```

Verify checksums when `checksums.txt` is available:

```sh
sha256sum -c checksums.txt
```

## Configure A Provider

Global configuration goes in `~/.config/ub/config.yaml`. A repository can also
provide `.ub/config.yaml`, which overrides global settings for that workspace.

OpenAI example:

```yaml
default_provider: openai
default_model: gpt-4o-mini
providers:
  openai:
    type: openai
    api_key: ${OPENAI_API_KEY}
```

OpenAI-compatible local endpoint example:

```yaml
default_provider: local
default_model: openai/gpt-oss-20b
providers:
  local:
    type: openai-compat
    base_url: http://127.0.0.1:8000/v1
    timeout: 10m
```

Development fake provider example:

```yaml
default_provider: fake
default_model: fake/demo
providers:
  fake:
    type: fake
    script:
      - type: text_delta
        text: fake response
      - type: done
```

Check the merged effective config:

```sh
ub config show
ub config path
ub doctor --plain
```

## Run ub

Headless agent:

```sh
ub run -p "inspect this repository and summarize the CLI commands"
```

Direct provider chat:

```sh
ub chat "say PONG"
```

TUI:

```sh
ub
ub --resume
ub --resume=<session-id>
```

Session and rollout inspection:

```sh
ub sessions ls
ub rollout show <session-id>
ub rollout show <session-id> --turns 5..10
ub rollout show <session-id> --json
```

## Storage, Logs, And Cleanup

Default paths:

- Sessions: `$XDG_DATA_HOME/ub/ub.db`, or `~/.local/share/ub/ub.db`.
- State: `$XDG_STATE_HOME/ub`, or `~/.local/state/ub`.
- TUI log: `$XDG_STATE_HOME/ub/ub.log`, or `~/.local/state/ub/ub.log`.
- Global permission rules: `~/.config/ub/permissions.yaml`.

Cleanup defaults:

```yaml
cleanup:
  enabled: true
  interval: 24h
  sessions:
    max_age: 720h
    min_recent_per_workspace: 20
  logs:
    max_size_mb: 10
    max_backups: 5
```

Startup cleanup is best effort. It prunes old sessions while retaining the most
recent sessions per workspace; events are removed by SQLite cascade when their
session is deleted. Tool-output spillover files are pruned from the state
directory according to `context.tool_results.spillover_max_age`.

Log rotation runs before opening the selected log file. Set `UB_LOG_FILE` to
choose a custom file and `UB_LOG_LEVEL=debug|info|warn|error` to control level.

