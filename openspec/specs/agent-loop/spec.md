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
