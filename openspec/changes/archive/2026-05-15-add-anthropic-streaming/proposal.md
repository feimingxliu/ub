## Why

I-08 已能调用 Anthropic 非流式 Messages API，但 CLI 仍只能在完整响应后输出。I-10 需要接入 Anthropic streaming，使 `ub chat` 能逐 token 输出，并为后续 TUI 流式渲染复用同一 provider stream。

## What Changes

- 扩展 `internal/provider/anthropic`，使用 SDK streaming API。
- 默认 Anthropic Chat 走 streaming，按 delta 转换为 provider `text_delta` 事件。
- stream 结束时返回 `usage` 和 `done` 事件。
- 支持 context 取消和 `Close()` 安全关闭。
- 增加 httptest/VCR 风格的流式测试。

## Capabilities

### New Capabilities

- `anthropic-streaming`: Anthropic provider 的 streaming 调用、delta 转换、取消与关闭行为。

### Modified Capabilities

- `anthropic-provider`: Anthropic provider 从非流式补齐 streaming 输出能力。

## Impact

- 修改 `internal/provider/anthropic` 的 `Chat` 实现和能力标记。
- 增加流式响应测试，不引入新外部依赖。
- 不实现 tool call streaming；I-21 负责。
