## ADDED Requirements

### Requirement: OpenAI 工具调用

OpenAI provider SHALL 在 request 包含工具定义时向 Chat Completions API 发送 tools，并支持内部 `tool_use` 与 `tool_result` content block 转换。streaming delta 中的 tool_calls MUST 聚合为完整 provider `tool_call` 事件，且 arguments JSON MUST 完整保留。

#### Scenario: tools 请求体

- **GIVEN** provider Request 包含 `read` 工具定义
- **WHEN** OpenAI provider 创建 ChatCompletion 请求
- **THEN** 请求 MUST 包含名称为 `read` 的 function tool schema

#### Scenario: streaming tool_calls 聚合

- **GIVEN** OpenAI streaming 响应分多段返回 tool call arguments
- **WHEN** 读取 provider stream
- **THEN** stream MUST 在 arguments 完整后返回 provider `tool_call` 事件

#### Scenario: tool_result 消息转换

- **GIVEN** provider Request 包含内部 tool_result block
- **WHEN** OpenAI provider 创建 ChatCompletion 请求
- **THEN** 请求 MUST 包含 role 为 `tool` 且关联 tool_call_id 的消息
