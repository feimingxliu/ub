## Why

I-01/I-02 已经提供 CLI 骨架和配置加载，下一步需要可持久化的会话元数据和统一的运行时日志，才能支撑后续 provider、rollout 与调试能力。按 roadmap，I-03/I-04 是 V0 基建收口前必须完成的存储与可观测性基础。

## What Changes

- 新增 SQLite store，包含最小 migration 机制、`sessions`/`events` 初始 schema，以及 session CRUD。
- 实现 `ub sessions ls`，按当前工作目录列出最近 session，空库输出友好提示。
- 新增全局 `slog` 初始化，支持 `UB_LOG_LEVEL` 和 `UB_LOG_FILE`。
- 默认 stderr 使用适合 CLI 的人类可读日志；设置 `UB_LOG_FILE` 时写 JSON 日志。
- 在 CLI 顶层统一 error 渲染，避免 Cobra 默认 usage 噪声；panic 时打印调用栈并以非零码退出。
- 不实现 events 写入/读取、rollout writer、metrics、tracing 或 sqlc。

## Capabilities

### New Capabilities

- `session-store`: SQLite 数据库、schema migration、session CRUD 与 `ub sessions ls` 行为。
- `runtime-logging`: 进程级日志初始化、环境变量配置、CLI 错误渲染与 panic recovery。

### Modified Capabilities

- 无。

## Impact

- 新增 `internal/store/`、`internal/session/` 或等价边界，以及 `internal/log/`。
- 修改 `internal/cli/` 和 `cmd/ub/`，接入 store 路径、`sessions ls`、日志初始化与 panic recovery。
- 新增 SQLite 依赖 `modernc.org/sqlite`。
- 新增 migration SQL 文件，默认数据库位置为 `~/.local/share/ub/ub.db`，测试使用临时文件。
- 增加单元测试和 CLI smoke，保持 `go test ./...`、`make lint`、`make build` 通过。
