# context-management Specification

## Purpose

定义 ub 的上下文体量估算、阈值判断和自动摘要行为。

## Requirements

### Requirement: Token 估算 API

系统 SHALL 在 `internal/context` 中提供 `Estimate(msgs []message.Message, model string) int`。该函数 MUST 接受 provider-neutral message 列表和模型名，并返回发起请求前可用的非负 token 估算值。

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
