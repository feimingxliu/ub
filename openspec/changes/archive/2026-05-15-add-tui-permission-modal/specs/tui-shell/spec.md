## ADDED Requirements

### Requirement: TUI 权限弹窗

TUI SHALL 在 permission Manager 请求 human approval 时显示阻塞式 modal。modal MUST 显示工具名、风险等级、执行模式、参数摘要；若请求包含 preview，modal MUST 显示 preview summary，并支持按 `d` 展开/折叠 unified diff。

#### Scenario: 显示 exec 审批

- **GIVEN** permission Manager 请求审批 `bash` 工具
- **WHEN** TUI 收到审批请求
- **THEN** modal MUST 显示工具名、风险等级、执行模式和参数摘要

#### Scenario: 展开 preview diff

- **GIVEN** 审批请求包含 preview summary 和 unified diff
- **WHEN** 用户按 `d`
- **THEN** modal MUST 展示 unified diff

### Requirement: TUI 权限决策按键

权限 modal SHALL 支持五个决策按键：`1` Allow once、`2` Deny、`3` Always cmd、`4` Always tool、`5` Always tool global。按键后 TUI MUST 把对应 `permission.Decision` 返回给等待中的 permission Asker，并关闭 modal。

#### Scenario: Allow once

- **WHEN** 用户在权限 modal 中按 `1`
- **THEN** Asker MUST 返回 `permission.DecisionAllow`

#### Scenario: Deny

- **WHEN** 用户在权限 modal 中按 `2`
- **THEN** Asker MUST 返回 `permission.DecisionDeny`

#### Scenario: Always global

- **WHEN** 用户在权限 modal 中按 `5`
- **THEN** Asker MUST 返回 `permission.DecisionAlwaysGlobal`

### Requirement: TUI 审批上下文提示

权限 modal SHALL 在特殊上下文中显示额外提示：Plan 模式审批 exec 时 MUST 提示命令可能仍有副作用；agent-approve 回退 human 时 MUST 展示 approval agent reason。

#### Scenario: Plan exec 警告

- **GIVEN** 请求 mode 为 `plan` 且 risk 为 `exec`
- **WHEN** modal 渲染
- **THEN** 输出 MUST 包含 `Plan mode: command may still have side effects`

#### Scenario: approval agent reason

- **GIVEN** 请求包含 approval agent 回退原因
- **WHEN** modal 渲染
- **THEN** 输出 MUST 包含该 reason
