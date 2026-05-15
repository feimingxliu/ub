## ADDED Requirements

### Requirement: 权限决策模型

系统 SHALL 在 `internal/permission` 中定义 `Decision`，包含 `Allow`、`Deny`、`AlwaysCmd`、`AlwaysTool`、`AlwaysGlobal` 五种值。系统 MUST 定义 `Request`，包含 tool 名称、args、risk、execution mode、可选 preview、command/cwd 以及上下文摘要。系统 MUST 定义 `Asker` 接口用于 human approval。

#### Scenario: human allow once

- **GIVEN** 未命中任何规则的 exec 请求
- **WHEN** human Asker 返回 `Allow`
- **THEN** Manager MUST 返回 allowed 结果，且不新增 always-rule

#### Scenario: human deny

- **GIVEN** 未命中任何规则的 exec 请求
- **WHEN** human Asker 返回 `Deny`
- **THEN** Manager MUST 返回 denied 结果，且不执行后续自动放行

### Requirement: mode gate 集成

权限 Manager SHALL 在任何规则或审批之前调用 execution mode gate。`plan` 模式下的 write 风险请求 MUST 被拒绝，且 MUST NOT 调用 human Asker 或 approval agent。

#### Scenario: plan 拒绝 write 且不询问

- **GIVEN** Manager 配置了会计数的 mock Asker
- **WHEN** 在 `plan` 模式下提交 `RiskWrite` 请求
- **THEN** Manager MUST 返回 denied 结果，且 mock Asker 调用次数为 0

### Requirement: exec 审批路径

权限 Manager SHALL 对 `RiskExec` 请求执行审批。`default` 和 `plan` 模式下，若未命中 allow-rule，MUST 调用 human Asker。`agent-approve` 模式下，若未命中 allow-rule，MUST 先调用 approval agent；approval agent 返回 allow 时 MUST 不调用 human Asker；返回 deny、unsure 或错误时 MUST 回退 human Asker。

#### Scenario: default exec 询问 human

- **GIVEN** `default` 模式的 exec 请求未命中规则
- **WHEN** Manager 处理该请求
- **THEN** Manager MUST 调用 human Asker 并采用其决策

#### Scenario: approval agent allow

- **GIVEN** `agent-approve` 模式的 exec 请求未命中规则，approval agent 返回 allow
- **WHEN** Manager 处理该请求
- **THEN** Manager MUST 返回 allowed 结果，且 MUST NOT 调用 human Asker

#### Scenario: approval agent unsure 回退 human

- **GIVEN** `agent-approve` 模式的 exec 请求未命中规则，approval agent 返回 unsure
- **WHEN** Manager 处理该请求
- **THEN** Manager MUST 调用 human Asker 并采用 human 决策

### Requirement: session allow-rule

权限 Manager SHALL 支持 session 级 always-rule，保存在内存中。human Asker 返回 `AlwaysCmd` 时，后续同 tool 且同 command 的请求 MUST 自动 allowed；返回 `AlwaysTool` 时，后续同 tool 的请求 MUST 自动 allowed。session 级规则 MUST 不写入磁盘。

#### Scenario: AlwaysCmd 后续命中

- **GIVEN** 第一次 bash command `git status` 请求由 human 返回 `AlwaysCmd`
- **WHEN** 第二次提交同 tool 且同 command 的请求
- **THEN** Manager MUST 自动 allowed，且不再调用 human Asker

#### Scenario: AlwaysTool 后续命中

- **GIVEN** 第一次 `bash` 请求由 human 返回 `AlwaysTool`
- **WHEN** 第二次提交任意 `bash` command
- **THEN** Manager MUST 自动 allowed，且不再调用 human Asker

### Requirement: global allow-rule 持久化

权限 Manager SHALL 支持 global always-rule。human Asker 返回 `AlwaysGlobal` 时，Manager MUST 追加一条 global rule 到 `permissions.yaml`，后续新 Manager 加载同一路径后 MUST 命中该规则并自动 allowed。写入 MUST 使用临时文件加 rename，避免中途失败破坏已有文件。

#### Scenario: AlwaysGlobal 跨 Manager 生效

- **GIVEN** 第一个 Manager 对 `bash` 请求收到 human `AlwaysGlobal`
- **WHEN** 创建第二个 Manager 并加载同一个 `permissions.yaml`
- **THEN** 第二个 Manager 处理同 tool 请求时 MUST 自动 allowed，且不调用 human Asker

#### Scenario: 原子写失败不破坏旧文件

- **GIVEN** `permissions.yaml` 已存在合法内容
- **WHEN** 规则保存过程在 rename 前失败
- **THEN** 原文件内容 MUST 保持不变，后续加载 MUST 仍成功

### Requirement: 黑名单优先级

权限 Manager SHALL 内置危险命令黑名单，至少匹配 `rm\s+-rf\s+/`、`mkfs\.`、`dd\s+.*of=/dev/`。黑名单命中时，Manager MUST 跳过 global/session allow-rule 和 approval agent，并强制调用 human Asker。

#### Scenario: 黑名单绕过 global rule

- **GIVEN** 已有允许 `bash` 的 global rule
- **WHEN** 提交 command 为 `rm -rf /` 的 `bash` exec 请求
- **THEN** Manager MUST 调用 human Asker，不能直接由 global rule 放行

#### Scenario: 黑名单绕过 approval agent

- **GIVEN** `agent-approve` 模式且 approval agent 会返回 allow
- **WHEN** 提交 command 为 `dd if=a of=/dev/sda` 的 exec 请求
- **THEN** Manager MUST 不调用 approval agent，必须调用 human Asker

### Requirement: preview 透传

权限 Manager SHALL 在调用 human Asker 时保留 `Request.Preview`。如果请求带有 preview，Asker 收到的 Request MUST 指向同一份 preview 数据。

#### Scenario: Asker 收到 preview

- **GIVEN** 请求包含一个 `tool.Preview`
- **WHEN** Manager 需要调用 human Asker
- **THEN** mock Asker MUST 能读取到该 preview 的 summary 和 file diff
