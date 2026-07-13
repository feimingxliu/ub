## Why

ub 目前只按单一 token 阈值把早期消息整体摘要，虽然已经能避免部分 context overflow，但无法说明为何压缩、无法优先清除低价值工具输出，也难以保证目标、计划、用户纠正和最近验证结果在长任务中被保留。参考 `.references/oh-my-pi` 的可观察 compaction/pruning 流程，将上下文维护升级为分阶段、可解释的决策机制，可以降低长会话溢出和重要状态丢失的风险。

## What Changes

- 在发送主模型请求前构造统一的上下文快照和决策，覆盖正常保留、语义裁剪、分阶段摘要、overflow 后摘要并重试等路径。
- 按工具结果的价值和覆盖关系进行安全裁剪，保留完整 user turn、tool call/result 配对、当前工作状态和对后续执行有决定性作用的事实。
- 将摘要改为结构化的工作续接摘要，明确记录目标、已完成工作、关键文件和决策、验证结果、阻塞项、用户纠正与下一步；超出摘要模型预算时按完整 turn 分块并递归合并。
- 为每次裁剪或压缩写入可审计的决策数据，并通过运行时状态和本地 prompt 检查暴露原因、边界、预算变化与保护内容。
- 保持既有 `/compact`、自动阈值 compact、ContextWindowResolver 优先级和 session 恢复语义兼容；不引入图片归档或远程遥测依赖。

## Capabilities

### New Capabilities

- 无。

### Modified Capabilities

- `context-management`: 将单阈值 summary 扩展为基于快照的多阶段上下文决策、语义裁剪、结构化分块摘要与安全重试。
- `rollout-events`: 为 summary/compaction 事件增加可重建、可审计的决策与裁剪元数据。
- `prompt-sections`: 将 compact summary 模板及其无工具约束纳入可检查的 prompt section 边界。

## Impact

主要影响 `internal/agent` 的请求准备、摘要与历史恢复流程，`internal/context` 的预算和决策模型，rollout 事件/SQLite 持久化，以及 TUI/CLI 的 context 状态与 `/compact` 提示。配置保持兼容，新增的审计字段和派生数据不应包含 prompt 正文、API key 或其他敏感信息。
