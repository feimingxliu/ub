## ADDED Requirements

### Requirement: Tool result rollout 事件

rollout SHALL 支持 `tool_result` 事件类型。事件 payload MUST 包含 tool_use_id、tool 名称、输出文本、错误标记和可选文件变更摘要。Agent 执行工具后 MUST 写入该事件。

#### Scenario: 创建 tool_result 事件

- **WHEN** 调用 rollout helper 创建 tool_result 事件
- **THEN** 事件类型 MUST 为 `tool_result`，payload MUST 可 JSON 序列化并包含 tool_use_id 与 output

#### Scenario: 读取历史包含 tool_result

- **GIVEN** rollout 中包含 user_message、assistant_message 和 tool_result
- **WHEN** Agent 读取历史准备下一轮 provider request
- **THEN** tool_result MUST 能恢复为内部 message 的 tool_result block
