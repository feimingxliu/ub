## MODIFIED Requirements

### Requirement: Anthropic 非流式 Chat

Anthropic provider 的 `Chat` SHALL 使用 Anthropic Messages API 完成一次请求，并返回 provider stream。I-10 起默认 MUST 使用 streaming API；非流式响应转换 helper MAY 保留用于测试或未来 fallback。stream MUST 至少按顺序返回文本 delta、usage（如响应包含 token 用量）和 done 事件。

#### Scenario: 文本响应转换

- **GIVEN** Anthropic API 返回一个 text content block `pong`
- **WHEN** 调用 `Stream.Next`
- **THEN** 第一个事件 MUST 为 `text_delta`，文本为 `pong`

#### Scenario: usage 转换

- **GIVEN** Anthropic API 响应包含输入和输出 token 数
- **WHEN** 读取 provider stream
- **THEN** stream MUST 返回 `usage` 事件并保留 token 数

#### Scenario: done 结束

- **WHEN** 文本与 usage 事件都已读取
- **THEN** stream MUST 返回 `done` 事件，后续读取结束
