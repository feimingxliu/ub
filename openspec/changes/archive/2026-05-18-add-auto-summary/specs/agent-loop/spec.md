## ADDED Requirements

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
