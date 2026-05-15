## Context

`ub chat` 当前每次调用都会新建 session，只发送当前 prompt。I-14 需要让它能继续已有 session，便于在无 TUI 的阶段验证 provider、session 和 rollout 的连续对话路径。

## Goals / Non-Goals

**Goals:**

- 支持 `--session <id>` 继续已有 session。
- 支持 `--new` 强制新建 session。
- 从 rollout 中重建 user/assistant 历史消息并追加当前 user 消息后请求 provider。
- 新 turn 追加到原 session，并更新 session metadata。
- 让常见错误更贴近用户语言。

**Non-Goals:**

- 不实现 TUI `/model` 或 session picker。
- 不实现 summary 历史压缩；历史按已持久化 user/assistant 消息原样重建。
- 不实现 provider 模型列表校验；真实 provider 的 model 错误仍来自 provider/API。

## Decisions

- **session 选择只显式继续。** I-14 不自动继续最近 session；没有 `--session` 时默认新建，`--new` 主要用于拒绝与 `--session` 同时使用并表达意图。
- **history 只读取 message 事件。** 使用 `user_message` 与 `assistant_message` payload 重建 provider-neutral messages，忽略 usage/error。
- **turn 由已有事件最大 turn + 1 得出。** 这样旧 session 即使中间有 error，也能在下一轮稳定追加。
- **错误包装在 CLI 层。** provider factory、session lookup 和 provider stream 错误在 `runChat` 路径包装为可读上下文，底层 error 仍保留。

## Risks / Trade-offs

- **长历史可能变大** -> Sprint 4 的 context/summary 负责压缩；I-14 保持简单。
- **旧 error 事件不回灌模型** -> 当前只恢复对话消息，避免把错误文本误作为模型上下文。
- **并发继续同一 session** -> I-14 不做锁；后续 agent runtime 再统一串行化 session turns。
