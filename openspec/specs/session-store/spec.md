# session-store Specification

## Purpose

Define how `ub` stores session metadata, applies SQLite schema migrations, and lists sessions for the current workspace.

## Requirements

### Requirement: SQLite store 打开与默认路径

系统 SHALL 提供 SQLite store，并 MUST 默认把数据库放在用户数据目录的 `ub/ub.db` 下。若设置 `XDG_DATA_HOME`，默认路径 MUST 为 `$XDG_DATA_HOME/ub/ub.db`；否则 MUST 为 `~/.local/share/ub/ub.db`。

#### Scenario: 使用 XDG_DATA_HOME

- **GIVEN** 环境变量 `XDG_DATA_HOME=/tmp/data`
- **WHEN** 调用 `store.DefaultPath()`
- **THEN** 返回路径 `/tmp/data/ub/ub.db`

#### Scenario: 打开 store 自动创建父目录

- **WHEN** 调用 `store.Open(path)` 且 `path` 的父目录不存在
- **THEN** store MUST 创建父目录并成功打开 SQLite 数据库

### Requirement: Schema migration

系统 SHALL 在打开 store 时自动执行 migration。系统 MUST 维护 `schema_version` 表，按 migration 文件名顺序执行未应用的 SQL，并保证重复打开同一数据库不会重复应用已完成 migration。

#### Scenario: 首次打开创建表

- **WHEN** 对空数据库调用 `store.Open(path)`
- **THEN** 数据库包含 `schema_version`、`sessions`、`events` 表，以及 `sessions(workspace, updated_at)` 和 `events(session_id, turn, time)` 相关索引

#### Scenario: migration 幂等

- **WHEN** 对同一路径连续调用两次 `store.Open(path)`
- **THEN** 第二次打开不返回错误，且 `schema_version` 中每个 migration 只记录一次

#### Scenario: SQLite PRAGMA 生效

- **WHEN** store 打开成功
- **THEN** 数据库 MUST 启用 `foreign_keys=ON`、`journal_mode=WAL`、`synchronous=NORMAL`

### Requirement: Session CRUD

系统 SHALL 提供 session 元数据 CRUD：`CreateSession`、`GetSession`、`UpdateSession`、`DeleteSession`。不存在的 session 查询、更新或删除 MUST 返回可判定的 `ErrNotFound`。

#### Scenario: 创建并读取 session

- **WHEN** 调用 `CreateSession` 写入包含 `ID`、`Workspace`、`Title`、`Model`、`CreatedAt`、`UpdatedAt` 的 session，随后调用 `GetSession(id)`
- **THEN** 返回的 session 字段与写入值一致

#### Scenario: 更新 session

- **WHEN** 已存在 session，调用 `UpdateSession` 修改 `Title`、`Model`、`UpdatedAt`
- **THEN** 随后 `GetSession(id)` 返回更新后的字段

#### Scenario: 删除 session

- **WHEN** 已存在 session，调用 `DeleteSession(id)` 后再调用 `GetSession(id)`
- **THEN** `GetSession(id)` 返回 `ErrNotFound`

#### Scenario: 不存在 session 返回 ErrNotFound

- **WHEN** 对不存在的 ID 调用 `GetSession`、`UpdateSession` 或 `DeleteSession`
- **THEN** 返回值 MUST 可通过 `errors.Is(err, store.ErrNotFound)` 判定

### Requirement: 按 workspace 列出 session

系统 SHALL 支持按 workspace 列出最近 session。列表 MUST 只包含请求 workspace 的 session，并 MUST 按 `UpdatedAt` 降序排列。

#### Scenario: 仅返回指定 workspace

- **GIVEN** 数据库中存在 workspace `/repo/a` 和 `/repo/b` 的 session
- **WHEN** 调用 `ListSessions(ctx, "/repo/a", 20)`
- **THEN** 返回列表只包含 `/repo/a` 的 session

#### Scenario: 按更新时间倒序

- **GIVEN** 同一 workspace 下有多个 session，`UpdatedAt` 不同
- **WHEN** 调用 `ListSessions(ctx, workspace, 20)`
- **THEN** 返回列表按 `UpdatedAt` 从新到旧排序

#### Scenario: limit 生效

- **GIVEN** 同一 workspace 下有 3 个 session
- **WHEN** 调用 `ListSessions(ctx, workspace, 2)`
- **THEN** 返回 2 个最新 session

### Requirement: `ub sessions ls` 子命令

`ub sessions ls` SHALL 打开默认 store，并按当前工作目录列出 session。空列表 MUST 输出 `no sessions` 并以 exit code 0 退出。

#### Scenario: 空库输出 no sessions

- **GIVEN** 默认数据库不存在或当前工作目录没有 session
- **WHEN** 用户运行 `ub sessions ls`
- **THEN** stdout 输出 `no sessions`，命令成功退出

#### Scenario: 列出当前工作目录 session

- **GIVEN** 默认数据库中存在当前工作目录的 session
- **WHEN** 用户运行 `ub sessions ls`
- **THEN** stdout 包含该 session 的 ID、更新时间、标题和模型

#### Scenario: 不列出其他 workspace

- **GIVEN** 默认数据库中只存在其他工作目录的 session
- **WHEN** 用户运行 `ub sessions ls`
- **THEN** stdout 输出 `no sessions`

### Requirement: `ub sessions rm` 子命令

`ub sessions rm <session-id> [session-id...]` SHALL 打开默认 store，并按 ID 删除指定 session。命令 MUST 支持 `delete` 和 `del` 作为 alias。删除不存在的 session MUST 返回可读错误。

#### Scenario: 删除指定 session

- **GIVEN** 默认数据库中存在 ID 为 `sess_1` 的 session
- **WHEN** 用户运行 `ub sessions rm sess_1`
- **THEN** 该 session MUST 被删除，stdout MUST 包含 `deleted sess_1`

#### Scenario: 删除不存在 session

- **WHEN** 用户运行 `ub sessions delete missing`
- **THEN** 命令 MUST 返回说明 session 不存在的可读错误

### Requirement: `ub sessions clear` 子命令

`ub sessions clear --yes` SHALL 删除当前工作目录下的所有 session。命令 MUST NOT 删除其他 workspace 的 session。未提供 `--yes` 时，命令 MUST 拒绝执行并返回可读错误。

#### Scenario: 需要显式确认

- **WHEN** 用户运行 `ub sessions clear`
- **THEN** 命令 MUST 返回要求传入 `--yes` 的可读错误，且不删除任何 session

#### Scenario: 清理当前 workspace

- **GIVEN** 默认数据库中存在当前工作目录的两个 session，另一个工作目录的一个 session
- **WHEN** 用户运行 `ub sessions clear --yes`
- **THEN** 当前工作目录的两个 session MUST 被删除，另一个工作目录的 session MUST 保留，stdout MUST 包含 `deleted 2 sessions`
