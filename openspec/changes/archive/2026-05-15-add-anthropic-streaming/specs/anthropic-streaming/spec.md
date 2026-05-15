## ADDED Requirements

### Requirement: Anthropic streaming Chat

Anthropic provider 的 `Chat` SHALL 使用 Anthropic streaming Messages API，并返回一个懒读取 provider stream。provider capabilities MUST 标记 streaming 可用。

#### Scenario: capabilities 标记 streaming

- **WHEN** 查询 Anthropic provider `Caps()`
- **THEN** `SupportsStreaming` MUST 为 true

#### Scenario: Chat 创建 streaming 请求

- **GIVEN** 配置中 `base_url` 指向测试 server
- **WHEN** 调用 `Chat`
- **THEN** provider MUST 向 Anthropic streaming endpoint 发送请求

### Requirement: Streaming delta 转换

Anthropic provider SHALL 把 Anthropic text delta 转换为 provider `text_delta` 事件，并保持顺序。

#### Scenario: 多段文本 delta

- **GIVEN** Anthropic streaming 响应包含文本 delta `po` 和 `ng`
- **WHEN** 调用方连续读取 provider stream
- **THEN** 依次得到两个 `text_delta` 事件，拼接结果为 `pong`

### Requirement: Streaming usage 与 done

Anthropic provider SHALL 在 streaming 结束时返回 usage 事件（如 SDK accumulator 提供用量）和 done 事件。done 后继续读取 MUST 结束。

#### Scenario: usage 后 done

- **GIVEN** streaming 响应包含 usage
- **WHEN** 文本 delta 读取完毕
- **THEN** 后续事件 MUST 包含 `usage` 和 `done`

#### Scenario: done 后 EOF

- **WHEN** 调用方读取到 `done` 后再次调用 `Next`
- **THEN** stream MUST 返回 EOF

### Requirement: Streaming 取消与关闭

Anthropic provider stream SHALL 支持 context 取消和安全关闭。取消后 `Next` MUST 返回 context 错误；`Close` MUST 可重复调用且不 panic。

#### Scenario: context 取消

- **WHEN** 调用方取消传给 `Next` 的 context
- **THEN** stream MUST 关闭底层 SDK stream 并返回 context 取消错误

#### Scenario: Close 幂等

- **WHEN** 调用方连续调用 `Close`
- **THEN** 两次调用 MUST 都不返回错误
