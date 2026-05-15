# ollama-provider Specification

## Purpose

Define the Ollama provider used for local `/api/chat` streaming conversations.

## Requirements

### Requirement: Ollama provider 创建

系统 SHALL 提供 `internal/provider/ollama` provider，并通过 provider 工厂注册 `type: ollama`。provider MUST 支持 `base_url`、`headers` 和 `timeout`；未配置 `base_url` 时 MUST 使用 `http://localhost:11434`。

#### Scenario: 工厂创建 ollama provider

- **GIVEN** 配置项 `providers.ollama.type=ollama`
- **WHEN** 调用 provider 工厂创建 `ollama`
- **THEN** 返回 provider 名称为 `ollama`，且 capabilities 标记 streaming 可用

#### Scenario: 默认 base_url

- **WHEN** 创建 ollama provider 时 `base_url` 为空
- **THEN** provider MUST 使用 `http://localhost:11434` 作为默认 endpoint

### Requirement: Ollama `/api/chat` streaming

Ollama provider 的 `Chat` SHALL 调用 `${base_url}/api/chat`，发送 text-only messages，并把 NDJSON 响应转换为 provider 事件。

#### Scenario: 请求体

- **GIVEN** 请求包含 model 与 user 文本消息
- **WHEN** 调用 Ollama provider `Chat`
- **THEN** HTTP 请求体 MUST 包含 `model`、`stream: true` 和 `messages`

#### Scenario: 多段文本 delta

- **GIVEN** Ollama NDJSON 响应包含 message content `po` 和 `ng`
- **WHEN** 读取 provider stream
- **THEN** 依次得到两个 text_delta，拼接为 `pong`

#### Scenario: usage 与 done

- **GIVEN** Ollama done 行包含 `prompt_eval_count` 和 `eval_count`
- **WHEN** 读取到结束
- **THEN** stream MUST 返回 usage 事件后返回 done 事件

#### Scenario: done 后 EOF

- **WHEN** 读取到 done 后再次调用 `Next`
- **THEN** stream MUST 返回 EOF

### Requirement: Ollama 消息限制

Ollama provider SHALL 支持 system/user/assistant 文本消息。非文本 content block MUST 返回可读错误。

#### Scenario: 非文本 block

- **WHEN** 请求包含 image、tool_use 或 tool_result block
- **THEN** Ollama provider MUST 返回错误说明当前文本 provider 不支持该 block
