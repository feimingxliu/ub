## Context

当前仓库已有 Cobra CLI、配置加载与 JSON Schema，`sessions ls` 仍是 I-03 占位错误，CLI 顶层错误也还没有统一渲染。后续 provider、rollout 与 TUI 都依赖可复用的 session 元数据存储和稳定的运行时日志。

## Goals / Non-Goals

**Goals:**

- 使用 `modernc.org/sqlite` 提供纯 Go SQLite store。
- 启动时自动执行幂等 migration，创建 `schema_version`、`sessions`、`events` 表。
- 提供 session CRUD，并让 `ub sessions ls` 列出当前 workspace 的最近 session。
- 初始化全局 `slog`，支持 `UB_LOG_LEVEL`、`UB_LOG_FILE`。
- CLI 顶层统一 error 输出和 panic recovery，输出可读错误和调用栈。

**Non-Goals:**

- 不实现 event append/read、rollout writer/reader 或 `ub rollout show`。
- 不引入 sqlc、metrics、tracing、日志轮转或远程日志。
- 不做 session resume、session 创建策略、agent loop 绑定 session。
- 不改变已有 config loader 语义。

## Decisions

### 1. Store 边界与数据位置

新增 `internal/store`，导出 `Open(path string) (*Store, error)`、`DefaultPath() (string, error)`、`Session` 与 CRUD 方法。CLI 只依赖 store 包，不直接拼 SQL。

默认路径遵循 XDG 数据目录：优先 `$XDG_DATA_HOME/ub/ub.db`，否则 `~/.local/share/ub/ub.db`。这与设计文档的默认路径一致，同时让测试可通过 `XDG_DATA_HOME` 隔离真实用户数据。

备选方案是把路径放进 YAML config。本次不采用，因为 I-03 只需要最小可用 store，配置项会扩大 schema 和迁移面。

### 2. Migration 机制

使用 `embed` 打包 `internal/store/migrations/*.sql`，按文件名排序执行。`schema_version(version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL)` 记录已执行 migration。每个 migration 在事务中执行；Open 时设置 `PRAGMA foreign_keys=ON`、`journal_mode=WAL`、`synchronous=NORMAL`。

`001_init.sql` 同时创建 `sessions` 和 `events`。events 本次只建表和索引，不提供写入/读取 API，避免提前实现 I-09。

### 3. Session API 语义

`Session` 字段保持 roadmap 关键签名，并保留 `Summary` 字段以匹配设计 SQL。时间以 Unix 毫秒存储，读写时转换为 `time.Time`。`CreateSession` 要求调用方提供 ID、Workspace、CreatedAt、UpdatedAt；空时间可由 store 补当前时间。`GetSession`、`UpdateSession`、`DeleteSession` 对不存在 ID 返回 `ErrNotFound`。

`ListSessions(ctx, workspace, limit)` 按 `updated_at DESC` 返回，`limit <= 0` 使用默认 20，上限 100，避免 CLI 误读超大库。

### 4. `ub sessions ls`

`sessions ls` 获取当前工作目录作为 workspace，打开默认 DB，调用 `ListSessions`。空列表输出 `no sessions`。非空时输出稳定表格列：`ID UPDATED TITLE MODEL`。标题为空显示 `(untitled)`，模型为空显示 `-`。

### 5. 日志初始化与 CLI 错误路径

新增 `internal/log`，包名可使用 `logx` 避免与标准库 `log` 混淆。`SetupFromEnv(stderr io.Writer) (*slog.Logger, func() error, error)` 解析：

- `UB_LOG_LEVEL`: `debug`、`info`、`warn`、`error`，空值默认为 `info`，非法值返回错误。
- `UB_LOG_FILE`: 为空时 stderr 使用人类可读 text handler；非空时打开文件追加 JSON 日志，stderr 仍保留 CLI 错误输出。

`cmd/ub/main.go` 或 `cli.Execute` 在命令执行前初始化 logger 并设为 `slog.SetDefault`。CLI 执行开始时写一条 debug 日志，确保 `UB_LOG_LEVEL=debug ./ub config show 2>&1 | grep -i debug` 可验证。

### 6. Panic recovery

把可测试逻辑拆成 `cli.Run(args []string, stdout, stderr io.Writer) int`；`Execute()` 只调用 `Run` 并 `os.Exit(code)`。`Run` 顶层 `defer recover`，panic 时向 stderr 打印 `panic: <value>` 和 `debug.Stack()`，返回非零码。普通命令 error 统一打印 `error: <message>`，不打印 Cobra usage。

## Risks / Trade-offs

- [Risk] `modernc.org/sqlite` 依赖体积较大，首次下载较慢 → 使用纯 Go 依赖换取无 cgo 分发，验证阶段运行 `go mod tidy` 和 `go test ./...`。
- [Risk] migration 出错可能留下半应用状态 → 单个 migration 使用事务，记录 version 与 SQL 执行同事务提交。
- [Risk] CLI 测试误写真实用户 DB → 测试必须设置 `XDG_DATA_HOME` 指向 `t.TempDir()`，store 测试只用临时文件。
- [Risk] debug 日志污染命令 stdout → 日志只写 stderr 或 `UB_LOG_FILE`，结构化命令输出继续写 stdout。
