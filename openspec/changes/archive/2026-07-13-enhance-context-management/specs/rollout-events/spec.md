## MODIFIED Requirements

### Requirement: Summary rollout 事件

rollout SHALL 支持 `summary` 事件类型。事件 payload MUST 包含摘要文本、被压缩消息数量、保留消息数量、维护前估算 token、最终 provider context，以及可选的上下文决策审计：动作、原因、维护后 token、cut boundary、裁剪/保护的 tool use ID、summary model、耗时和 retry 状态。自动、手动和 overflow 上下文维护成功后 MUST 写入该事件。所有新增审计字段 MUST 为可选，以便旧 SQLite event 保持可读。

#### Scenario: 创建带决策审计的 summary 事件

- **WHEN** Agent 成功执行 threshold summary 或 overflow compact-and-retry
- **THEN** 事件类型 MUST 为 `summary`
- **AND** payload MUST 可 JSON 序列化并包含摘要文本、最终 provider context、决策动作、原因和 token before/after
- **AND** payload MUST NOT 包含原始 prompt、API key 或完整被裁剪工具输出

#### Scenario: prune-only 事件可恢复 provider context

- **GIVEN** Agent 已执行安全 tool-result pruning 且无需 summary
- **WHEN** Agent 写入上下文维护 rollout 事件并在后续 session 恢复
- **THEN** payload MUST 保存包含裁剪占位结果的最终 provider context
- **AND** 恢复后的下一次 provider request MUST 使用该 context 而非被裁剪结果的原始输出

#### Scenario: prune-only checkpoint 不重复会话搜索

- **GIVEN** prune-only summary payload 保存的 provider context 以已有 user message 开头
- **WHEN** 用户运行 `ub sessions search`
- **THEN** 系统 MUST NOT 使用该 payload 的 Messages 作为搜索正文或额外匹配

#### Scenario: 读取新旧 summary 事件

- **GIVEN** rollout 同时包含没有 audit/messages 的旧 summary 事件和带完整 context 的新 summary 事件
- **WHEN** Agent 读取历史准备下一轮 provider request
- **THEN** 新事件 MUST 恢复其保存的完整 provider context
- **AND** 旧事件 MUST 至少恢复为 system summary message
