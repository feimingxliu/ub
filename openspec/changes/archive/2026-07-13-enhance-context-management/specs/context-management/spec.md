## MODIFIED Requirements

### Requirement: 自动 Summary 触发

系统 SHALL 在 Agent 发起 provider 请求前估算当前请求消息的 token 数，并以有效 ContextWindowResolver 窗口、输出保留预算、完整 user-turn 边界和候选工具结果构造可观察的上下文快照。若有效窗口大于 0 且 `estimated_tokens + reserve_output_tokens` 的比例大于 `context.trigger_ratio`，系统 MUST 生成 `threshold` 上下文决策：先执行安全语义裁剪；若裁剪后仍未回到目标预算，系统 MUST 触发自动 structured summary。未知窗口 MUST 不因阈值触发自动维护。

#### Scenario: 阈值裁剪后无需 summary

- **GIVEN** 有效上下文窗口为 100、输出保留为 0、`context.trigger_ratio` 为 0.8
- **AND** 当前请求估算为 81 token 且早期存在可安全裁剪的 tool result
- **WHEN** Agent 在主 provider 请求前执行上下文决策
- **THEN** Agent MUST 先裁剪该结果并重新估算请求
- **AND** 若重新估算不大于 80 token，Agent MUST 按裁剪后的消息发起请求且 MUST NOT 调用 summary provider

#### Scenario: 阈值裁剪不足时触发 summary

- **GIVEN** 有效上下文窗口为 100、输出保留为 0、`context.trigger_ratio` 为 0.8
- **AND** 当前请求估算为 81 token 且不存在足以降至 80 token 的安全裁剪候选
- **WHEN** Agent 在主 provider 请求前执行上下文决策
- **THEN** Agent MUST 在主 provider 请求前执行 structured summary
- **AND** 主 provider 请求 MUST 使用摘要和保留窗口组成的 context

#### Scenario: 未知窗口不自动维护

- **GIVEN** ContextWindowResolver 未返回有效最大上下文
- **WHEN** Agent 准备 provider 请求并完成 token 估算
- **THEN** Agent MUST 上报估算用量
- **AND** Agent MUST NOT 仅因 token 估算触发自动裁剪或 summary

### Requirement: 自动 Summary 历史压缩

系统 SHALL 使用无工具的 structured summary prompt 生成摘要。成功后，系统 MUST 把被压缩的早期消息替换为单条 system summary message，并保留最近 `context.keep_recent_turns` 个完整 user turn 及其后续消息；`keep_recent_turns` 未配置或小于 1 时 MUST 使用默认值 3。summary MUST 合并已有 conversation summary 而不是堆叠，并保留用户意图与完成条件、约束和用户纠正、进度/计划、关键文件与决策、验证或错误、当前状态和下一步。

摘要输入和递归归并 MUST 按 summary provider/model 的可用窗口预算，并只能在完整 user turn 边界分块。系统 MUST NOT 在 tool_use/tool_result 配对中间切分，且单个完整 user turn 无法放入 summary 预算时 MUST 返回可读错误并保持原 context 不变。

#### Scenario: 历史替换为 structured summary 加最近 3 轮

- **GIVEN** 历史中存在 5 个 user turn，配置 `keep_recent_turns` 为 3
- **WHEN** 自动 summary 成功
- **THEN** 主 provider 请求中的消息 MUST 以一条 system summary 开头
- **AND** 后续消息 MUST 保留最近 3 个完整 user turn 及其后续 assistant/tool 消息
- **AND** summary MUST 包含固定的工作续接字段而不凭空声明未验证结果

#### Scenario: 摘要模型预算要求分块归并

- **GIVEN** 待压缩的多个完整 user turn 超过 summary provider 的单次输入预算
- **AND** 每个完整 user turn 都可放入该预算
- **WHEN** 系统生成摘要
- **THEN** 系统 MUST 按完整 user turn 分块生成摘要并递归归并
- **AND** summary provider 请求 MUST 不携带工具定义

#### Scenario: 单个 turn 超过摘要预算

- **GIVEN** 一个完整 user turn 自身超过 summary provider 的输入预算
- **WHEN** 系统尝试自动或手动 summary
- **THEN** 系统 MUST 返回说明应配置更大 summary 窗口或减少输入的可读错误
- **AND** 系统 MUST NOT 改写当前 provider context

### Requirement: 手动 Compact 触发

系统 SHALL 支持在已有 session 历史上主动触发一次上下文 compact。手动 compact MUST 跳过 `context.trigger_ratio` 判断，但 MUST 构造 `manual` 上下文决策、先应用安全语义裁剪并在存在可压缩前缀时执行 structured summary；它 MUST 使用现有 summary provider、summary prompt、`context.keep_recent_turns` 和带完整 provider context 的 rollout `Summary` 事件格式。没有可维护前缀时系统 MUST 保持历史不变并返回可读结果。

#### Scenario: 手动 compact 压缩早期历史

- **GIVEN** 当前 session 历史中存在超过 `context.keep_recent_turns` 的 user turn
- **WHEN** 用户触发手动 compact
- **THEN** 系统 MUST 生成 structured summary
- **AND** session provider context MUST 变为一条 system summary 加最近 `context.keep_recent_turns` 个完整 user turn 及其后续消息
- **AND** rollout MUST 写入一条包含 manual 决策的 `Summary` 事件

#### Scenario: 手动 compact 无可维护前缀

- **GIVEN** 当前 session 历史中的 user turn 数量不超过 `context.keep_recent_turns`
- **AND** 不存在可安全裁剪的旧 tool result
- **WHEN** 用户触发手动 compact
- **THEN** 系统 MUST 保持 session 历史不变
- **AND** 系统 MUST 返回可读提示说明当前没有可压缩内容

## ADDED Requirements

### Requirement: 上下文决策与安全语义裁剪

系统 SHALL 使用确定性的 `ContextSnapshot` 和 `ContextDecision` 统一普通请求、手动 compact 与 provider overflow 的上下文维护。decision MUST 使用稳定动作 `keep`、`prune`、`compact` 或 `compact-and-retry` 之一，以及稳定原因 `manual`、`threshold`、`overflow`、`incomplete` 或 `mid_turn` 之一。相同 snapshot MUST 得到相同 decision。

系统 MUST 保护当前 user turn、摘要保留窗口、完整 tool_use/tool_result 配对、错误结果、最近验证结果和包含文件变更的结果。系统只可裁剪可证明低价值的成功 `read` / `grep` 结果：输出明确为空，或被后续相同输入的 read/grep 结果完整覆盖。裁剪 MUST 用同一 `tool_use_id` 的 provider-neutral 占位 tool result 保留配对；完整 transcript、rollout 与 spillover 中的原始结果 MUST 不被删除。

#### Scenario: 覆盖的 read 结果被安全裁剪

- **GIVEN** 一个旧 read tool result 与同一输入的后续成功 read tool result 配对完整
- **AND** 旧 result 不在当前 user turn 或保留窗口内，后续 result 仍保留在 provider context 中
- **WHEN** 规划器生成 threshold 或 manual decision
- **THEN** decision MUST 将旧 result 的 tool_use_id 标记为 pruned
- **AND** provider context MUST 保留该 ID 的 tool result block 并以裁剪占位内容替换原输出

#### Scenario: 受保护的错误与验证结果不被裁剪

- **GIVEN** 早期 tool result 为错误、最近验证结果或包含文件变更
- **WHEN** 规划器生成任何 decision
- **THEN** decision MUST 将该结果标记为 protected
- **AND** 执行后 provider context MUST 保留其原始输出

#### Scenario: mid-turn 不破坏 tool 配对

- **GIVEN** 当前 context 包含尚未有对应 tool result 的 tool_use
- **WHEN** Agent 在该 turn 中尝试上下文维护
- **THEN** decision MUST 使用 `mid_turn` 原因并选择 `keep` 或延后处理
- **AND** 系统 MUST NOT 删除、裁剪或 summary 该不完整配对

#### Scenario: overflow 只重试一次

- **GIVEN** provider 返回可识别的 context overflow
- **WHEN** Agent 尚未为当前 provider request 执行 overflow recovery
- **THEN** Agent MUST 生成 `compact-and-retry` decision 并在成功维护后重试一次
- **AND** 如果重试仍返回 context overflow，Agent MUST 返回 provider 错误而不再次维护或重试

### Requirement: 上下文决策可观察性

系统 SHALL 在每次实际裁剪、summary 或 overflow recovery 时报告 decision、原因、维护前后估算 token、cut boundary、裁剪和保护的 tool use ID、summary model、耗时与 retry 状态。活动事件和本地诊断 MUST 仅暴露这些元数据与简短状态，不得暴露原始 prompt、完整工具输出、API key 或 provider endpoint 的敏感部分。

#### Scenario: 裁剪决策可解释

- **GIVEN** Agent 在请求前裁剪了一个被覆盖的 tool result
- **WHEN** TUI 或本地诊断读取该次 context maintenance 状态
- **THEN** 输出 MUST 显示动作、threshold 原因、token before/after 以及被裁剪和受保护的 tool use ID
- **AND** 输出 MUST NOT 默认包含被裁剪的原始输出正文

#### Scenario: 保留决策不伪造维护记录

- **GIVEN** snapshot 的 decision 为 `keep`
- **WHEN** Agent 发起 provider 请求
- **THEN** 系统 MUST 上报通常的 context usage
- **AND** 系统 MUST NOT 写入伪造的 summary 或 pruning rollout 记录
