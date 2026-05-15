## Why

当前 `ub chat` 能调用 provider，但对话过程不会落盘，无法恢复、调试或审计。I-09 需要把用户消息、助手消息、usage 和错误事件写入 SQLite rollout，为后续 session resume、agent loop 和 trace 查看打基础。

## What Changes

- 新增 `internal/rollout` 事件类型、Writer 和 Reader。
- 基于现有 SQLite `events` 表实现按 session 追加和按 session 顺序读取。
- 事件类型先覆盖 `user_message`、`assistant_message`、`usage`、`error`。
- `ub chat` 创建/绑定 session，并把每轮 provider 对话写入 rollout。
- 更新 `ub sessions ls` 可看到 chat 创建的 session。

## Capabilities

### New Capabilities

- `rollout-events`: rollout 事件模型、SQLite 写入读取、耐久性和迭代读取行为。

### Modified Capabilities

- `provider-runtime`: `ub chat` 需要把用户消息、助手输出、usage 和错误写入 rollout，并绑定 session。

## Impact

- 新增 `internal/rollout/` 包。
- 修改 CLI chat 运行路径，接入 store/session 与 rollout writer。
- 复用现有 SQLite store 和 `events` 表，不新增外部依赖。
- 不实现 tool 事件、summary 事件或漂亮打印。
