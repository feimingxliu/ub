## ADDED Requirements

### Requirement: TUI 活动流展示

TUI SHALL 在消息列表中展示 Agent 活动事件，包括模型 thinking、工具调用生命周期、权限审批结果和错误状态。活动流 MUST 使用紧凑前缀或图标与用户消息、assistant 正文区分；活动内容 MUST 按终端宽度换行，并随聊天区历史一起滚动。同一个 tool call 的 queued/running/done/failed 更新 MUST 合并到同一活动行，避免刷屏。

#### Scenario: thinking 活动渲染

- **GIVEN** TUI 收到 `thinking` 活动事件
- **WHEN** TUI 渲染消息列表
- **THEN** 输出 MUST 包含一条与 assistant 正文区分的思考状态消息
- **THEN** 该消息 MUST NOT 被合并进 assistant 最终回复文本

#### Scenario: 工具生命周期渲染

- **GIVEN** TUI 依次收到工具运行中和工具完成活动事件
- **WHEN** TUI 渲染消息列表
- **THEN** 输出 MUST 显示工具名、当前状态和短摘要
- **THEN** 输出 MUST NOT 直接展示完整 tool input JSON
- **THEN** 同一个 tool call MUST NOT 因状态变化产生多条重复活动消息

#### Scenario: 权限结果渲染

- **GIVEN** TUI 收到 approval agent 的权限活动事件
- **WHEN** TUI 渲染消息列表
- **THEN** 输出 MUST 显示审批来源、allow/deny/unsure/error 决策和原因
- **THEN** 若后续收到 human approval 决策，输出 MUST 显示最终用户决策

#### Scenario: 活动流参与滚动

- **GIVEN** 活动消息和普通聊天消息总高度超过当前聊天区
- **WHEN** 用户按 PageUp 或滚动鼠标
- **THEN** TUI MUST 能在聊天区内查看更早的活动消息

### Requirement: 活动流降噪与安全展示

TUI SHALL 对活动消息执行降噪展示：单条活动默认显示一行摘要，长文本 MUST 截断或换行到可见宽度，敏感值 MUST 显示为遮蔽文本。TUI MUST NOT 把 provider 未返回的隐藏推理链展示为真实 thinking 内容。

#### Scenario: 长活动摘要可读

- **GIVEN** 活动事件包含超过终端宽度的摘要
- **WHEN** TUI 渲染该活动
- **THEN** 摘要 MUST 在当前宽度内换行或截断
- **THEN** 右侧内容 MUST NOT 只能通过终端宿主横向不可见区域查看

#### Scenario: 敏感值不展示

- **GIVEN** 活动事件摘要中包含已遮蔽的 secret 值
- **WHEN** TUI 渲染该活动
- **THEN** 输出 MUST 保留遮蔽结果
- **THEN** 输出 MUST NOT 还原或展示原始 secret
