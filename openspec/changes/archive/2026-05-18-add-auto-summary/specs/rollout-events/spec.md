## ADDED Requirements

### Requirement: Summary rollout 事件

rollout SHALL 支持 `summary` 事件类型。事件 payload MUST 包含摘要文本、被压缩消息数量、保留消息数量和触发时的估算 token 数。Agent 自动 summary 成功后 MUST 写入该事件。

#### Scenario: 创建 summary 事件

- **WHEN** 调用 rollout helper 创建 summary 事件
- **THEN** 事件类型 MUST 为 `summary`
- **THEN** payload MUST 可 JSON 序列化并包含摘要文本

#### Scenario: 读取历史包含 summary

- **GIVEN** rollout 中包含 summary 事件和后续 user/assistant 事件
- **WHEN** Agent 读取历史准备下一轮 provider request
- **THEN** summary 事件 MUST 恢复为 system message
