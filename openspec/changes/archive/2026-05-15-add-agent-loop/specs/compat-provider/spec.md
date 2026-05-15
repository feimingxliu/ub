## ADDED Requirements

### Requirement: OpenAI 兼容工具调用

OpenAI 兼容 provider SHALL 复用 OpenAI provider 的工具定义、tool_use/tool_result 消息转换和 streaming tool_call 聚合行为。

#### Scenario: compat tools 请求体

- **GIVEN** 配置了 `type: openai-compat` 的 provider，Request 包含工具定义
- **WHEN** provider 创建 ChatCompletion 请求
- **THEN** 请求 MUST 与 OpenAI provider 使用同样的 tools 结构

#### Scenario: compat tool_result 转换

- **GIVEN** provider Request 包含内部 tool_result block
- **WHEN** OpenAI 兼容 provider 创建请求
- **THEN** 请求 MUST 包含 role 为 `tool` 的消息
