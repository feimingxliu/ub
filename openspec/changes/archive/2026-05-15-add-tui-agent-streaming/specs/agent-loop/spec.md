## ADDED Requirements

### Requirement: Agent 运行事件

Agent SHALL 支持可选运行事件回调。调用方配置回调后，Agent MUST 在文本增量、工具调用开始、工具调用结束、完成和错误时发出事件；未配置回调时现有行为 MUST 保持不变。

#### Scenario: 文本增量事件

- **GIVEN** provider stream 返回两个 text delta
- **WHEN** Agent 运行时配置了事件回调
- **THEN** 回调 MUST 收到两个 `DeltaText` 事件，顺序与 provider stream 一致

#### Scenario: 工具调用事件

- **GIVEN** provider stream 返回一个 tool call，工具执行成功
- **WHEN** Agent 运行时配置了事件回调
- **THEN** 回调 MUST 先收到 `ToolCallStart`，工具执行后收到 `ToolCallEnd`

#### Scenario: 完成事件

- **GIVEN** Agent 正常完成一次请求
- **WHEN** Agent 返回 Result
- **THEN** 回调 MUST 收到 `Done` 事件
