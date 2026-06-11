# openai-provider Specification

## Purpose

Define the OpenAI Chat Completions provider adapter and its text-only streaming behavior.

## Requirements

### Requirement: OpenAI provider 创建

系统 SHALL 提供 `internal/pkg/llm/provider/openai` provider，并通过 provider 工厂注册 `type: openai`。provider MUST 支持配置中的 `api_key`、`base_url`、`headers` 和 `timeout`。

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

### Requirement: OpenAI reasoning effort 映射

OpenAI provider SHALL 在 provider Request 包含 reasoning effort 且模型能力允许时，将该 effort 映射到 OpenAI Chat Completions 请求参数。若 Request 不包含 reasoning 或 effort 为 `none`，provider MUST NOT 发送 reasoning 参数。

#### Scenario: 发送 reasoning effort

- **GIVEN** provider Request 的 reasoning effort 为 `high`
- **WHEN** OpenAI provider 构造 Chat Completions 请求
- **THEN** 请求 MUST 包含 OpenAI reasoning effort `high`

#### Scenario: none 不发送 reasoning 参数

- **GIVEN** provider Request 的 reasoning effort 为 `none`
- **WHEN** OpenAI provider 构造 Chat Completions 请求
- **THEN** 请求 MUST NOT 包含 reasoning effort 参数
