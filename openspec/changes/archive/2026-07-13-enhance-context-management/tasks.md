## 1. 上下文决策基础

- [x] 1.1 在 `internal/context` 定义可序列化的 ContextSnapshot、ContextDecision、动作/原因和 tool-result 候选模型。
- [x] 1.2 实现确定性 planner，覆盖 keep、threshold、manual、overflow、incomplete 和 mid-turn 决策。
- [x] 1.3 为 planner 补充单元测试：未知窗口、预算阈值、受保护结果、覆盖结果、未完成 tool pairing 与 overflow retry。

## 2. Agent 上下文维护

- [x] 2.1 从 provider context 构建快照，识别完整 turn、tool call/result 配对、受保护结果和保守可裁剪候选。
- [x] 2.2 实现保留 tool_use ID 的 provider-neutral tool-result 裁剪占位，并确保完整 transcript 不被改写。
- [x] 2.3 将 prepare、手动 Compact 和 overflow recovery 接入共同的决策/执行流程，按 prune 后 token 预算决定是否 summary 或单次重试。
- [x] 2.4 为自动、手动和 overflow 路径补充 fake-provider 集成测试，覆盖裁剪、structured summary、失败不变和 pairing 安全性。

## 3. 摘要与可观察性

- [x] 3.1 将 default/short structured summary 模板注册为 `compact_instructions`，并保持 summary provider 无工具调用和递归完整-turn 预算。
- [x] 3.2 扩展 summary rollout payload/helper，持久化 provider context 和决策审计，同时保持旧事件恢复兼容。
- [x] 3.3 为 context maintenance 发出简短活动/上下文诊断，并为 `ub prompt inspect --variant compact` 提供脱敏 manifest。
- [x] 3.4 为 rollout 恢复、compact manifest、默认脱敏和实际 summary request 模板一致性补充测试。

## 4. 文档与验证

- [x] 4.1 将上下文决策、裁剪、审计和 compact inspect 的已交付接口同步到 requirements、design 与 roadmap-v2。
- [x] 4.2 运行受影响包测试、`make schema`（如配置类型变化）、`make test`、`make lint`、`make check` 和 CLI smoke tests。
- [x] 4.3 对照 OpenSpec 规格逐项审计实现，更新任务状态并执行严格 OpenSpec 校验。

## 5. Review 修复

- [x] 5.1 修复 reserve-aware threshold 计算，避免重复应用输出保留预算。
- [x] 5.2 将内置 `grep` 纳入保守 source-search 裁剪候选，并在实际 prune estimate 仍超预算时回退 compact。
- [x] 5.3 排除 prune-only checkpoint 的 session 搜索正文，避免恢复 context 造成重复匹配。
- [x] 5.4 同步规格并运行针对性与完整验证。
