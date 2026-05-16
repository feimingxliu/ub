## ADDED Requirements

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
