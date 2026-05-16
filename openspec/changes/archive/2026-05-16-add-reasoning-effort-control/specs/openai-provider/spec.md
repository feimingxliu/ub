## ADDED Requirements

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
