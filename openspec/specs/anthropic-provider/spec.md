# anthropic-provider Specification

## Purpose

Define the Anthropic provider adapter for Messages API calls, configuration handling, message conversion, and provider stream output.

## Requirements

### Requirement: Anthropic provider 创建

系统 SHALL 提供 `internal/pkg/llm/provider/anthropic` provider，并通过 provider 工厂注册 `type: anthropic`。provider MUST 支持配置中的 `api_key`、`base_url`、`headers` 和 `timeout`。

#### Scenario: 工厂创建 anthropic provider

- **GIVEN** 配置项 `providers.anthropic.type=anthropic`
- **WHEN** 调用 provider 工厂创建 `anthropic`
- **THEN** 返回的 provider 名称为 `anthropic`，且能力描述表明非流式调用可用

#### Scenario: 未配置 API key

- **WHEN** 创建 anthropic provider 时 `api_key` 为空
- **THEN** provider 创建 MUST 返回可读错误

#### Scenario: base_url 覆盖

- **GIVEN** 配置中设置 `base_url` 指向测试 HTTP server
- **WHEN** 调用 provider Chat
- **THEN** HTTP 请求 MUST 发送到配置的 base URL

#### Scenario: headers 注入

- **GIVEN** 配置中设置额外 header `x-org-id: org-1`
- **WHEN** 调用 provider Chat
- **THEN** HTTP 请求 MUST 包含该 header

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

### Requirement: 消息转换限制

Anthropic provider SHALL 将内部 user/assistant 文本消息转换为 Anthropic messages，将 system 文本消息合并为 Anthropic system 指令。非文本 content block MUST 返回可读错误。

#### Scenario: user 文本消息

- **WHEN** 请求包含一个 user 文本消息 `hello`
- **THEN** Anthropic 请求 MUST 包含 role 为 user 的文本 message

#### Scenario: system 文本消息

- **WHEN** 请求包含 system 文本消息
- **THEN** Anthropic 请求 MUST 把该文本放入 system 指令，而不是普通 messages 列表

#### Scenario: 非文本 block

- **WHEN** 请求包含 image、tool_use 或 tool_result block
- **THEN** Anthropic provider MUST 返回错误说明当前非流式文本 provider 不支持该 block

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

### Requirement: Anthropic thinking budget 映射

Anthropic provider SHALL 在 provider Request 包含 reasoning effort 且模型能力允许时，将 effort 映射为 Anthropic Messages API 的 thinking budget。`none` MUST 不发送 thinking；非 none effort MUST 使用正数 budget，并确保 thinking budget 小于请求的 max tokens。

#### Scenario: 发送 thinking budget

- **GIVEN** provider Request 的 reasoning effort 为 `high`
- **WHEN** Anthropic provider 构造 Messages 请求
- **THEN** 请求 MUST 启用 thinking 并设置正数 budget

#### Scenario: none 不发送 thinking

- **GIVEN** provider Request 的 reasoning effort 为 `none`
- **WHEN** Anthropic provider 构造 Messages 请求
- **THEN** 请求 MUST NOT 包含 thinking 配置
