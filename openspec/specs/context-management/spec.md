# context-management Specification

## Purpose

定义 ub 的上下文体量估算、阈值判断和自动摘要行为。

## Requirements

### Requirement: Token 估算 API

系统 SHALL 在 `internal/pkg/llm/context` 中提供 `Estimate(msgs []message.Message, model string) int`。该函数 MUST 接受 provider-neutral message 列表和模型名，并返回发起请求前可用的非负 token 估算值。

#### Scenario: 已知 OpenAI 字符串估算

- **WHEN** 调用 `Estimate` 估算单条 user 文本消息 `hello world`
- **THEN** 返回值 MUST 大于纯空消息开销，并且 MUST 稳定等于单元测试中记录的 OpenAI 系估算值

#### Scenario: 空消息估算

- **WHEN** 调用 `Estimate(nil, model)`
- **THEN** 返回值 MUST 等于 0

### Requirement: 多类型消息估算

系统 SHALL 把消息 role、文本 block、tool_use block 和 tool_result block 纳入估算。估算 MUST 保持 provider-neutral，不依赖具体 provider SDK 的消息结构。

#### Scenario: 工具消息计入估算

- **WHEN** 消息包含 tool_use input JSON 和 tool_result output
- **THEN** `Estimate` 返回值 MUST 大于只包含同一 role 的空文本消息估算值

### Requirement: 非 OpenAI 模型回退估算

系统 SHALL 在模型没有可用 tiktoken encoding 时使用字符近似估算。回退估算 MUST 不返回错误，并且 MUST 对同一输入保持确定性。

#### Scenario: 未知模型回退

- **WHEN** 调用 `Estimate` 估算未知模型的一条文本消息
- **THEN** 函数 MUST 返回大于 0 的确定性估算值

### Requirement: Usage 校正

系统 SHALL 支持根据 provider 返回的输入 usage 校正同一模型的后续估算。校正 MUST 是进程内、按模型隔离的，并且 MUST 忽略无效的 estimated 或 actual 值。

#### Scenario: usage 提高后续估算

- **GIVEN** 某模型的一次估算值低于 provider 返回的实际 input usage
- **WHEN** 调用 usage 观察接口记录该差异
- **THEN** 同一模型后续 `Estimate` 的返回值 MUST 高于校正前的返回值

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
