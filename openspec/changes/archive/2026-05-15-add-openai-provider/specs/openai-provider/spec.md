## ADDED Requirements

### Requirement: OpenAI provider 创建

系统 SHALL 提供 `internal/provider/openai` provider，并通过 provider 工厂注册 `type: openai`。provider MUST 支持配置中的 `api_key`、`base_url`、`headers` 和 `timeout`。

#### Scenario: 工厂创建 openai provider

- **GIVEN** 配置项 `providers.openai.type=openai`
- **WHEN** 调用 provider 工厂创建 `openai`
- **THEN** 返回 provider 名称为 `openai`，且 capabilities 标记 streaming 可用

#### Scenario: 未配置 API key

- **WHEN** 创建 openai provider 时 `api_key` 为空
- **THEN** provider 创建 MUST 返回可读错误

### Requirement: OpenAI streaming Chat

OpenAI provider 的 `Chat` SHALL 默认使用 streaming ChatCompletion，并按顺序返回 `text_delta`、usage（如可用）和 done 事件。

#### Scenario: 多段文本 delta

- **GIVEN** OpenAI streaming 响应包含 delta `po` 和 `ng`
- **WHEN** 读取 provider stream
- **THEN** 依次得到两个 text_delta，拼接为 `pong`

#### Scenario: done 后 EOF

- **WHEN** 读取到 done 后再次调用 `Next`
- **THEN** stream MUST 返回 EOF

### Requirement: OpenAI 非流式转换

OpenAI provider SHALL 支持非流式 ChatCompletion 响应转换，用于测试或 fallback。非流式转换 MUST 返回文本、usage 和 done 事件。

#### Scenario: 非流式文本响应

- **GIVEN** OpenAI 非流式响应 message content 为 `pong`
- **WHEN** 转换为 provider events
- **THEN** 第一个事件 MUST 为 `text_delta: pong`

### Requirement: OpenAI 消息转换限制

OpenAI provider SHALL 支持 system/user/assistant 文本消息。非文本 content block MUST 返回可读错误。

#### Scenario: system role 转换

- **WHEN** 请求包含 system 文本消息
- **THEN** OpenAI 请求 MUST 包含 role 为 system 的 message

#### Scenario: 非文本 block

- **WHEN** 请求包含 image、tool_use 或 tool_result block
- **THEN** OpenAI provider MUST 返回错误说明当前文本 provider 不支持该 block
