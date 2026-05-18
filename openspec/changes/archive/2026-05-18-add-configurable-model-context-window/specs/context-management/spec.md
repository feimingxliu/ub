## ADDED Requirements

### Requirement: 模型级上下文窗口优先级

系统 SHALL 在判断自动 summary 阈值和上报 context used/max/% 时优先使用当前模型配置的 `max_context_tokens`。当当前模型未配置有效的 `max_context_tokens` 时，系统 MUST 回退到 provider `Caps().MaxContextTokens`；当二者都未知时，系统 MUST 跳过自动 summary 阈值判断并仅上报 used tokens。

#### Scenario: 自动 summary 使用模型级上下文窗口

- **GIVEN** provider 默认最大上下文为 128000，当前模型配置 `max_context_tokens: 200000`
- **WHEN** 当前请求 token 估算为 170000，`context.trigger_ratio` 为 0.8
- **THEN** Agent MUST 不触发自动 summary

#### Scenario: context status 使用模型级上下文窗口

- **GIVEN** 当前模型配置 `max_context_tokens: 200000`
- **WHEN** Agent 上报请求 token 估算为 100000
- **THEN** runtime event MUST 包含 max tokens 200000 和 ratio 0.5

#### Scenario: 未配置模型级窗口时回退 provider caps

- **GIVEN** 当前模型未配置 `max_context_tokens`，provider 最大上下文为 128000
- **WHEN** Agent 上报请求 token 估算为 64000
- **THEN** runtime event MUST 包含 max tokens 128000 和 ratio 0.5
