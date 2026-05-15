## Context

现有 store migration 已创建 `sessions` 和 `events` 表，I-07/I-08 已有 `ub chat` 与 provider stream。I-09 的重点是把对话事件以稳定 JSON payload 写入 `events`，并提供可测试的读取接口。

## Goals / Non-Goals

**Goals:**

- 定义 rollout `Event`、`Type`、`Writer`、`Reader`。
- 基于 SQLite 单条 INSERT 实现事件追加，保持按 session/turn/time/id 顺序读取。
- `ub chat` 每次运行创建一个 session，写入 user、assistant、usage 和 error 事件。
- 让 `ub sessions ls` 能看到 chat session。

**Non-Goals:**

- 不实现已有 session 续聊；I-14 负责 `--session`/`--new`。
- 不实现 tool、summary、permission、mode switch 等事件。
- 不实现 rollout pretty printer 或导出命令。

## Decisions

- **rollout 包直接依赖 `*store.Store`。** 这样复用已有连接、migration 和 PRAGMA，避免重复打开数据库导致锁和生命周期混乱。
- **事件 payload 使用 `json.RawMessage`。** rollout 层不绑定具体 payload Go 类型；提供 `MarshalPayload` 辅助保持调用方简洁。
- **turn 由 CLI 传入。** I-09 只有单轮 chat，使用 `turn=1`；后续 agent loop/session resume 再负责 turn 递增策略。
- **assistant 文本聚合后写一次。** provider 流式消费时 stdout 仍逐 delta 输出，但 rollout 写一条聚合后的 assistant message，降低事件数量并符合当前 I-09 类型范围。
- **错误也落盘。** provider/stream 失败时追加 `error` 事件后返回原错误，便于调试失败 session。

## Risks / Trade-offs

- **单轮 chat 默认创建新 session** -> 后续 I-14 会补齐继续已有 session；当前先保证事件可见。
- **Reader 使用 callback 而不是 iter.Seq2** -> 保持 Go 1.25 兼容且简单；未来可在不破坏存储格式的情况下增加 iter API。
- **assistant delta 聚合会丢失 token 级时间线** -> I-09 只要求消息级事件，流式 token 级 trace 可在后续扩展。

## Migration Plan

无需新增 migration。部署后首次 `ub chat` 会在默认 store 中创建 session 并写入 events；回滚时删除 `internal/rollout` 和 chat 持久化调用即可。
