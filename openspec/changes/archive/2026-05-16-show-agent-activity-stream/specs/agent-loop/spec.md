## ADDED Requirements

### Requirement: Agent 活动事件

Agent SHALL 在可选事件回调中输出结构化活动事件，用于描述模型思考、工具调用生命周期、权限审批结果和非正文状态。活动事件 MUST 与 assistant 正文文本增量分离；未配置事件回调时，Agent 的最终 Result、tool_result 回灌和 rollout 写入行为 MUST 保持不变。

#### Scenario: reasoning delta 转换为 thinking 活动

- **GIVEN** provider stream 返回 reasoning 事件
- **WHEN** Agent 运行时配置了事件回调
- **THEN** 回调 MUST 收到表示 `thinking` 的活动事件
- **THEN** reasoning 文本 MUST NOT 混入最终 assistant 正文

#### Scenario: 工具生命周期活动

- **GIVEN** provider stream 返回一个 tool call
- **WHEN** Agent 准备执行该工具、完成权限检查并执行结束
- **THEN** 回调 MUST 收到包含工具名的活动事件，至少覆盖运行中和结束状态
- **THEN** 活动事件 MUST 包含安全的输入摘要或结果摘要，而不是完整原始 JSON

#### Scenario: 权限审批活动

- **GIVEN** Agent 运行在 `auto` 模式且 exec 工具需要 approval agent 判断
- **WHEN** approval agent 返回 allow、deny、unsure 或 error
- **THEN** 回调 MUST 收到 `permission` 活动事件，包含来源、决策、是否允许和原因
- **THEN** 若后续回退到 human approval，回调 MUST 再收到最终 human 决策活动事件

#### Scenario: 无事件回调时行为不变

- **GIVEN** Agent 未配置事件回调
- **WHEN** Agent 执行包含工具调用的一轮请求
- **THEN** Agent MUST 仍执行工具并返回最终 assistant 文本
- **THEN** stdout 或 Result 文本 MUST NOT 包含活动流内容

### Requirement: 工具活动摘要安全

Agent SHALL 为工具活动生成短摘要。摘要 MUST 使用字段白名单和长度限制；疑似 secret 的字段和值 MUST 被遮蔽。命令类工具摘要 MUST 至少保留命令首行和 cwd（如可用），文件类工具摘要 MUST 优先展示路径和变更数量。

#### Scenario: secret 字段被遮蔽

- **GIVEN** tool call input 包含 `api_key`、`token`、`password` 或 `authorization` 字段
- **WHEN** Agent 生成工具活动摘要
- **THEN** 摘要 MUST NOT 包含这些字段的原始值

#### Scenario: 长参数被截断

- **GIVEN** tool call input 包含超长文本字段
- **WHEN** Agent 生成工具活动摘要
- **THEN** 摘要 MUST 限制在固定长度内，并用截断标记表示内容被省略
