## MODIFIED Requirements

### Requirement: 权限决策模型

系统 SHALL 在 `internal/permission` 中定义 `Decision`，包含 `Allow`、`Deny`、`AlwaysCmd`、`AlwaysTool`、`AlwaysGlobal` 五种值。系统 MUST 定义 `Request`，包含 tool 名称、args、risk、execution mode、可选 preview、command/cwd、上下文摘要以及可选 approval agent 回退原因。系统 MUST 定义 `Asker` 接口用于 human approval。

#### Scenario: human allow once

- **GIVEN** 未命中任何规则的 exec 请求
- **WHEN** human Asker 返回 `Allow`
- **THEN** Manager MUST 返回 allowed 结果，且不新增 always-rule

#### Scenario: human deny

- **GIVEN** 未命中任何规则的 exec 请求
- **WHEN** human Asker 返回 `Deny`
- **THEN** Manager MUST 返回 denied 结果，且不执行后续自动放行

#### Scenario: approval reason 传给 human

- **GIVEN** agent-approve 模式下 approval agent 返回 unsure 并带 reason
- **WHEN** Manager 回退 human Asker
- **THEN** Asker 收到的 Request MUST 包含该 approval agent reason
