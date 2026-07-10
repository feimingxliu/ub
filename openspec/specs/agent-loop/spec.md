# agent-loop Specification

## Purpose
Define the headless provider/tool loop and `ub run -p` behavior.

## Requirements

### Requirement: Agent loop 执行模型

系统 SHALL 在 `internal/agent` 中提供 Agent，使用 provider、tool Registry、permission Manager、execution mode 和 rollout writer 执行单个用户请求。Agent MUST 按顺序处理 provider stream，最多执行 25 轮 provider/tool 循环；没有 tool_call 时 MUST 结束并返回最终 assistant 文本。

#### Scenario: fake provider 调用 read 后回答

- **GIVEN** fake provider 先返回 `tool_call(read)`，收到 tool_result 后返回文本答案
- **WHEN** Agent 运行一次用户请求
- **THEN** Agent MUST 执行 read 工具，把 tool_result 回灌给 provider，并返回最终 assistant 文本

#### Scenario: max turns 防护

- **GIVEN** provider 每轮都返回新的 tool_call
- **WHEN** Agent 连续达到 25 轮仍未结束
- **THEN** Agent MUST 返回 max turns 错误

### Requirement: 工具 dispatch 与权限

Agent SHALL 对每个 tool_call 通过 Registry 查找工具，并在 Execute 前调用 permission Manager。若工具实现 `PreviewableTool`，Agent MUST 先调用 Preview，并把同一份 preview 放入 permission Request。permission 拒绝或 mode gate 拒绝时 MUST 生成错误 tool_result，且 MUST NOT 调用 Execute。

#### Scenario: Preview 先于 Execute

- **GIVEN** 一个实现 PreviewableTool 的写工具
- **WHEN** Agent 收到该工具的 tool_call
- **THEN** Preview MUST 在 permission Ask 前调用，Execute MUST 只在 permission allow 后调用

#### Scenario: plan 模式拒绝 write

- **GIVEN** Agent 运行在 `plan` 模式，模型请求 edit 工具
- **WHEN** Agent 处理该 tool_call
- **THEN** Agent MUST 返回错误 tool_result，且目标文件内容 MUST 保持不变

### Requirement: `ub run -p`

CLI SHALL 提供 headless `ub run -p "<prompt>"`。命令 MUST 加载配置、选择 provider/model、注册本地工具、解析 `--mode`，并运行 Agent。`ub run` 成功时 MUST 把最终 assistant 文本写到 stdout。

#### Scenario: run prompt 参数

- **GIVEN** 配置中存在 fake provider，脚本会返回最终文本 `done`
- **WHEN** 用户运行 `ub run --provider fake -p "hi"`
- **THEN** stdout MUST 包含 `done`，命令返回成功

#### Scenario: mode 参数

- **WHEN** 用户运行 `ub run --mode plan -p "edit file"`
- **THEN** Agent MUST 以 `plan` execution mode 运行

### Requirement: Agent rollout 写入

Agent SHALL 把用户消息、assistant 消息、usage、tool_result 和错误写入 rollout。工具执行成功或失败后，Agent MUST 为 tool_result 写入 rollout 事件。

#### Scenario: tool_result 写入

- **GIVEN** Agent 执行了一个工具调用
- **WHEN** 该工具返回成功结果
- **THEN** rollout MUST 包含对应的 tool_result 事件，payload 保留 tool_use_id、tool 名称、输出和错误标记

### Requirement: Agent 运行事件

Agent SHALL 支持可选运行事件回调。调用方配置回调后，Agent MUST 在文本增量、结构化 activity、完成和错误时发出事件；未配置回调时现有行为 MUST 保持不变。activity MUST 覆盖 provider thinking、工具生命周期、权限审批和 notice/error 摘要。

#### Scenario: 文本增量事件

- **GIVEN** provider stream 返回两个 text delta
- **WHEN** Agent 运行时配置了事件回调
- **THEN** 回调 MUST 收到两个 `DeltaText` 事件，顺序与 provider stream 一致

#### Scenario: thinking activity

- **GIVEN** provider stream 返回 `reasoning_delta`
- **WHEN** Agent 运行时配置了事件回调
- **THEN** 回调 MUST 收到 kind 为 `thinking` 的 activity
- **THEN** reasoning 内容 MUST NOT 混入最终 assistant 文本

#### Scenario: 工具 lifecycle activity

- **GIVEN** provider stream 返回一个 tool call，工具执行成功
- **WHEN** Agent 运行时配置了事件回调
- **THEN** 回调 MUST 收到工具 queued/running/done activity，包含工具名和短摘要
- **THEN** 摘要 MUST NOT 直接展示完整 tool input JSON

#### Scenario: 权限 activity

- **GIVEN** auto 模式下 approval agent 返回 allow/deny/unsure/error，且可能回退 human approval
- **WHEN** Agent 处理需要审批的命令
- **THEN** 回调 MUST 输出 permission activity，包含来源、决策和原因

#### Scenario: 安全摘要

- **GIVEN** 工具参数或结果包含长文本或 secret-like 字段
- **WHEN** Agent 输出 tool activity
- **THEN** 摘要 MUST 使用字段白名单、长度限制和 secret 遮蔽

#### Scenario: 完成事件

- **GIVEN** Agent 正常完成一次请求
- **WHEN** Agent 返回 Result
- **THEN** 回调 MUST 收到 `Done` 事件

### Requirement: Agent 上下文准备

Agent SHALL 在每次主 provider 请求前准备上下文消息。准备过程 MUST 在用户消息追加后、主 provider `Chat` 调用前执行；若自动 summary 触发，主 provider MUST 只收到压缩后的消息。

#### Scenario: provider 收到压缩历史

- **GIVEN** Agent 历史超过 summary 阈值，且 summary provider 返回摘要文本
- **WHEN** Agent 调用主 provider
- **THEN** 主 provider request MUST 包含一条 system summary message
- **THEN** 主 provider request MUST 不包含被 summary 压缩的早期原始消息

#### Scenario: summary 使用 small_model

- **GIVEN** Agent 配置了 summary model `small`
- **WHEN** 自动 summary 触发
- **THEN** summary provider request MUST 使用模型 `small`

#### Scenario: summary 失败写入错误

- **GIVEN** 自动 summary 触发但 summary provider 返回错误
- **WHEN** Agent 运行该请求
- **THEN** Agent MUST 返回错误
- **THEN** rollout MUST 写入 error 事件
