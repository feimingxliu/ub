## 1. 依赖与目录骨架

- [x] 1.1 引入 `modernc.org/sqlite`，运行 `go mod tidy`
- [x] 1.2 创建 `internal/store/`、`internal/store/migrations/` 目录
- [x] 1.3 创建 `internal/log/` 目录（包名可用 `logx`）
- [x] 1.4 确认 `cmd/ub/` 与 `internal/cli/` 的执行入口可承载统一初始化和测试注入

## 2. SQLite migration 与 Store 打开

- [x] 2.1 编写 `internal/store/migrations/001_init.sql`，创建 `schema_version`、`sessions`、`events` 表和索引
- [x] 2.2 使用 `embed` 打包 migrations，并按文件名排序执行
- [x] 2.3 实现 `DefaultPath()`：`XDG_DATA_HOME/ub/ub.db` 优先，否则 `~/.local/share/ub/ub.db`
- [x] 2.4 实现 `Open(path string)`：创建父目录、打开 SQLite、设置 `foreign_keys=ON`、`journal_mode=WAL`、`synchronous=NORMAL`
- [x] 2.5 实现 migration 事务：只执行未记录的 migration，执行 SQL 和写入 `schema_version` 同事务提交
- [x] 2.6 实现 `Close() error`

## 3. Session CRUD

- [x] 3.1 定义 `Session`、`Store` 类型和 `ErrNotFound`
- [x] 3.2 实现 `CreateSession(ctx, Session) error`
- [x] 3.3 实现 `GetSession(ctx, id string) (*Session, error)`
- [x] 3.4 实现 `ListSessions(ctx, workspace string, limit int) ([]Session, error)`，按 `updated_at DESC` 排序并限制 limit
- [x] 3.5 实现 `UpdateSession(ctx, Session) error`
- [x] 3.6 实现 `DeleteSession(ctx, id string) error`
- [x] 3.7 统一 time 与 SQLite INTEGER 的 Unix 毫秒转换

## 4. `ub sessions ls`

- [x] 4.1 替换 `sessions ls` 的 I-03 占位错误，打开默认 store
- [x] 4.2 使用当前工作目录作为 workspace 调用 `ListSessions`
- [x] 4.3 空列表输出 `no sessions`
- [x] 4.4 非空列表输出稳定表格列 `ID UPDATED TITLE MODEL`
- [x] 4.5 为 CLI 测试提供可控的 `XDG_DATA_HOME` / 临时数据库路径隔离方式

## 5. 日志初始化

- [x] 5.1 实现 `internal/log` 的 `SetupFromEnv(stderr io.Writer)` 或等价函数
- [x] 5.2 支持 `UB_LOG_LEVEL=debug|info|warn|error`，非法值返回可读错误
- [x] 5.3 默认用 stderr text handler；`UB_LOG_FILE` 设置时追加写 JSON handler
- [x] 5.4 初始化后调用 `slog.SetDefault(logger)`，并返回 cleanup 关闭文件句柄
- [x] 5.5 在 CLI 命令执行开始处写 debug 日志，便于验证 debug level

## 6. CLI 错误渲染与 panic recovery

- [x] 6.1 将 CLI 入口拆成可测试的 `Run(args []string, stdout, stderr io.Writer) int` 和 `Execute()`
- [x] 6.2 设置 Cobra `SilenceUsage=true`、`SilenceErrors=true`
- [x] 6.3 普通 error 统一输出 `error: <message>` 到 stderr，并返回非零码
- [x] 6.4 顶层 `defer recover` 捕获 panic，输出 `panic:` 和 `debug.Stack()` 到 stderr
- [x] 6.5 保持 `--help` / 子命令 help 正常输出且 exit code 为 0

## 7. 测试

- [x] 7.1 `internal/store` 单测覆盖默认路径、父目录创建、PRAGMA、migration 幂等和表/索引存在
- [x] 7.2 `internal/store` 单测覆盖 session create/get/list/update/delete 和 `ErrNotFound`
- [x] 7.3 `internal/cli` 单测覆盖 `sessions ls` 空库、当前 workspace 过滤、非空表格输出
- [x] 7.4 `internal/log` 单测覆盖 level 解析、非法 level、stderr handler、`UB_LOG_FILE` JSON 输出
- [x] 7.5 `internal/cli` 单测覆盖普通 error 渲染、不输出 usage、panic recovery、help 正常退出

## 8. 验证与收尾

- [x] 8.1 运行 `gofmt` / `gofumpt` 覆盖新增 Go 文件
- [x] 8.2 运行 `go test ./...`
- [x] 8.3 运行 `make lint`
- [x] 8.4 运行 `make build`
- [x] 8.5 手测：`./ub sessions ls` 空库输出 `no sessions`
- [x] 8.6 手测：向临时数据库手动插入当前 workspace session 后，`./ub sessions ls` 能列出
- [x] 8.7 手测：`UB_LOG_LEVEL=debug ./ub config show 2>&1 | grep -i debug` 能看到 debug 日志，且 stdout 仍是配置 YAML
