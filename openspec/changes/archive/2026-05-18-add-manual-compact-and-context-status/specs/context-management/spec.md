## ADDED Requirements

### Requirement: 手动 Compact 触发

系统 SHALL 支持在已有 session 历史上主动触发一次上下文 compact。手动 compact MUST 使用现有 summary provider、summary prompt、`context.keep_recent_turns` 和 rollout `Summary` 事件格式；手动 compact MUST 跳过 `context.trigger_ratio` 判断，但 MUST 在没有可压缩前缀时保持历史不变并返回可读结果。

#### Scenario: 手动 compact 压缩早期历史

- **GIVEN** 当前 session 历史中存在超过 `context.keep_recent_turns` 的 user turn
- **WHEN** 用户触发手动 compact
- **THEN** 系统 MUST 生成 summary
- **THEN** session 历史 MUST 变为一条 system summary 加最近 `context.keep_recent_turns` 个 user turn 及其后续消息
- **THEN** rollout MUST 写入一条 `Summary` 事件

#### Scenario: 手动 compact 无可压缩前缀

- **GIVEN** 当前 session 历史中的 user turn 数量不超过 `context.keep_recent_turns`
- **WHEN** 用户触发手动 compact
- **THEN** 系统 MUST 保持 session 历史不变
- **THEN** 系统 MUST 返回可读提示说明当前没有可压缩内容

### Requirement: 上下文用量上报

系统 SHALL 在 Agent 准备 provider 请求时上报当前请求消息的 token 估算值。若 provider 声明 `MaxContextTokens` 大于 0，系统 MUST 同时上报最大上下文和使用比例；若最大上下文未知，系统 MUST 仍上报估算 token 数。

#### Scenario: 请求前上报上下文用量

- **GIVEN** provider 声明最大上下文为 100
- **WHEN** Agent 准备发起 provider 请求并估算当前消息为 25 token
- **THEN** Agent runtime event MUST 包含 used tokens 25
- **THEN** Agent runtime event MUST 包含 max tokens 100 和 ratio 0.25

#### Scenario: 最大上下文未知时上报 used tokens

- **GIVEN** provider 未声明最大上下文
- **WHEN** Agent 准备发起 provider 请求并完成 token 估算
- **THEN** Agent runtime event MUST 包含 used tokens
- **THEN** Agent runtime event MUST 不要求包含 context ratio
