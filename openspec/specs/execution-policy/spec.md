# execution-policy Specification

## Purpose
Define execution modes and mode-level gates before tool permission decisions.

## Requirements
### Requirement: 执行模式解析

系统 SHALL 在 `internal/pkg/core/execution` 中定义 `Mode`，支持 `work`、`plan`、`auto` 三个值。系统 MUST 提供解析函数，把空字符串视作 `work`，未知值 MUST 返回可读错误。

#### Scenario: 空字符串使用 work

- **WHEN** 调用模式解析函数并传入空字符串
- **THEN** 返回 `work` 且无错误

#### Scenario: 未知模式报错

- **WHEN** 调用模式解析函数并传入 `danger`
- **THEN** 返回包含未知模式名称的错误

### Requirement: mode gate

系统 SHALL 提供 mode gate，用于根据 `execution.Mode` 和 `tool.Risk` 决定是否允许继续进入权限审批流程。`plan` 模式 MUST 拒绝 `RiskWrite`；`work` 和 `auto` MUST 允许 `RiskWrite` 继续。`RiskExec` MUST 在三种模式下继续进入权限审批流程。

#### Scenario: plan 拒绝 write

- **WHEN** 在 `plan` 模式下检查 `RiskWrite`
- **THEN** mode gate MUST 返回拒绝错误，错误消息 MUST 说明 plan 模式只读

#### Scenario: exec 进入审批流程

- **WHEN** 在 `work`、`plan` 或 `auto` 模式下检查 `RiskExec`
- **THEN** mode gate MUST 不返回错误，由权限管理器继续处理审批
