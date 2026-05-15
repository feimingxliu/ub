## ADDED Requirements

### Requirement: Anthropic 工具调用

Anthropic provider SHALL 在 request 包含工具定义时向 Messages API 发送 tools，并支持内部 `tool_use` 与 `tool_result` content block 转换。streaming 响应中的 `tool_use` MUST 转换为 provider `tool_call` 事件，且 input JSON MUST 完整保留。

#### Scenario: tools 请求体

- **GIVEN** provider Request 包含 `read` 工具定义
- **WHEN** Anthropic provider 创建 Messages API 请求
- **THEN** 请求 MUST 包含名称为 `read` 的 tool schema

#### Scenario: tool_use stream 转换

- **GIVEN** Anthropic streaming 响应包含 tool_use block 和 input_json_delta
- **WHEN** 读取 provider stream
- **THEN** stream MUST 返回 provider `tool_call` 事件，包含 tool_use_id、tool 名称和完整 JSON input

#### Scenario: tool_result 消息转换

- **GIVEN** provider Request 包含内部 tool_result block
- **WHEN** Anthropic provider 创建 Messages API 请求
- **THEN** 请求 MUST 包含对应 tool_result content block
