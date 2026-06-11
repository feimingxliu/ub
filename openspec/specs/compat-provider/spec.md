# compat-provider Specification

## Purpose

Define the OpenAI-compatible provider used for local and third-party Chat Completions services.

## Requirements

### Requirement: OpenAI 兼容 provider 创建

系统 SHALL 提供 `internal/pkg/llm/provider/compat` provider，并通过 provider 工厂注册 `type: openai-compat`。provider MUST 支持 `base_url`、`api_key`、`headers` 和 `timeout`，且 `base_url` MUST 显式配置。

#### Scenario: 工厂创建 compat provider

- **GIVEN** 配置项 `providers.compat.type=openai-compat` 且配置了 `base_url`
- **WHEN** 调用 provider 工厂创建 `compat`
- **THEN** 返回 provider 名称为 `compat`，且 capabilities 标记 streaming 可用

#### Scenario: 未配置 base_url

- **WHEN** 创建 openai-compat provider 时 `base_url` 为空
- **THEN** provider 创建 MUST 返回可读错误

#### Scenario: API key 可选

- **GIVEN** 本地 OpenAI 兼容服务不需要鉴权
- **WHEN** 创建 openai-compat provider 时 `api_key` 为空
- **THEN** provider 创建 MUST 成功

### Requirement: OpenAI 兼容 streaming Chat

OpenAI 兼容 provider SHALL 使用 Chat Completions streaming 协议，并复用 OpenAI provider 的文本、usage 和 done 事件转换。

#### Scenario: 多段文本 delta

- **GIVEN** OpenAI 兼容 streaming 响应包含 delta `po` 和 `ng`
- **WHEN** 读取 provider stream
- **THEN** 依次得到两个 text_delta，拼接为 `pong`

#### Scenario: done 后 EOF

- **WHEN** 读取到 done 后再次调用 `Next`
- **THEN** stream MUST 返回 EOF

### Requirement: OpenAI 兼容消息限制

OpenAI 兼容 provider SHALL 支持 system/user/assistant 文本消息。非文本 content block MUST 返回可读错误。

#### Scenario: 非文本 block

- **WHEN** 请求包含 image、tool_use 或 tool_result block
- **THEN** OpenAI 兼容 provider MUST 返回错误说明当前文本 provider 不支持该 block

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
