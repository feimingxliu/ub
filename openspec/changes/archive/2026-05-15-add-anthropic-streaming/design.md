## Context

现有 Anthropic provider 已完成配置、消息转换和非流式响应封装。SDK 提供 `Messages.NewStreaming`，可以直接读取 Anthropic SSE 事件。provider 抽象已经是 `Stream.Next(ctx)` pull 模式，适合把 SDK stream 包成 provider stream。

## Goals / Non-Goals

**Goals:**

- Anthropic provider 默认使用 streaming API。
- 将 text delta 转为 `provider.EventTextDelta`。
- 将最终 usage 转为 `provider.EventUsage`，再返回 `provider.EventDone`。
- `Close()` 可中断并释放 SDK stream。

**Non-Goals:**

- 不解析 tool_use / tool_call streaming。
- 不暴露 Anthropic 原始 SSE 事件。
- 不改变 fake provider 或其他 provider。

## Decisions

- **Chat 返回懒读取 stream。** `Chat` 只创建 SDK stream，不预先读完整响应；`Next(ctx)` 每次推进 SDK stream。
- **事件缓冲用于 usage/done。** 收到 message stop 后，stream 把 usage 和 done 放入内部队列，保证调用方按 provider 统一事件读取。
- **非文本增量报错。** 若 SDK stream 返回不支持的内容块类型，本迭代返回错误，避免工具调用被静默忽略。
- **保留非流式转换 helper。** tests 和未来 fallback 可继续复用响应转事件逻辑，但主路径走 streaming。

## Risks / Trade-offs

- **SDK streaming 类型复杂** -> 以本地 SDK v1.43.0 的测试和类型为准，编译测试固定适配。
- **usage 出现时机依赖 SDK accumulator** -> 通过 httptest SSE 覆盖文本拼接、usage 和 done。
- **取消行为依赖底层 HTTP** -> `Next(ctx)` 先检查 ctx，并在取消时关闭 SDK stream。
