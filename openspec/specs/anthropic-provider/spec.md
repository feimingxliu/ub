# anthropic-provider Specification

## Purpose

Define the Anthropic provider adapter for Messages API calls, configuration handling, message conversion, and provider stream output.

## Requirements

### Requirement: Anthropic provider 创建

系统 SHALL 提供 `internal/provider/anthropic` provider，并通过 provider 工厂注册 `type: anthropic`。provider MUST 支持配置中的 `api_key`、`base_url`、`headers` 和 `timeout`。

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
