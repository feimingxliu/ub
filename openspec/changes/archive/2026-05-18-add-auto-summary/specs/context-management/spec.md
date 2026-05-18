## ADDED Requirements

### Requirement: 自动 Summary 触发

系统 SHALL 在 Agent 发起 provider 请求前估算当前请求消息的 token 数。若 provider 声明的 `MaxContextTokens` 大于 0，且 `Estimate(messages, model) / MaxContextTokens` 大于配置的 `context.trigger_ratio`，系统 MUST 触发自动 summary。

#### Scenario: 超过阈值触发 summary

- **GIVEN** provider 最大上下文为 100，配置 `context.trigger_ratio` 为 0.8
- **WHEN** 当前请求消息估算为 81 token
- **THEN** Agent MUST 在主 provider 请求前触发 summary

#### Scenario: 未超过阈值不触发 summary

- **GIVEN** provider 最大上下文为 100，配置 `context.trigger_ratio` 为 0.8
- **WHEN** 当前请求消息估算为 80 token
- **THEN** Agent MUST 不触发 summary，并按原始消息发起主 provider 请求

### Requirement: 自动 Summary 历史压缩

系统 SHALL 使用内嵌 summary prompt 和 `small_model` 生成摘要。触发成功后，系统 MUST 把被压缩的早期消息替换为单条 system summary message，并保留最近 `context.keep_recent_turns` 个 user turn 及其后续消息。`keep_recent_turns` 未配置或小于 1 时 MUST 使用默认值 3。

#### Scenario: 历史替换为 summary 加最近 3 轮

- **GIVEN** 历史中存在 5 个 user turn，配置 `keep_recent_turns` 为 3
- **WHEN** 自动 summary 成功
- **THEN** 主 provider 请求中的消息 MUST 以一条 system summary 开头
- **THEN** 后续消息 MUST 保留最近 3 个 user turn 及其后续 assistant/tool 消息

#### Scenario: 没有可压缩前缀时跳过 summary

- **GIVEN** 历史中 user turn 数量不超过 `keep_recent_turns`
- **WHEN** token 估算超过阈值
- **THEN** Agent MUST 跳过 summary，避免把全部历史替换为摘要

### Requirement: Usage 估算校正接入

系统 SHALL 在 provider stream 返回输入 usage 后，把本轮请求前估算值与实际 input usage 传给 token 估算校正接口。

#### Scenario: usage 回灌估算器

- **GIVEN** Agent 发起主 provider 请求前得到估算值
- **WHEN** provider stream 返回 input usage
- **THEN** 系统 MUST 调用 usage 校正接口更新同模型后续估算
