## Why

现有 `ub eval` 只能一次运行一个 task/model，无法高效建立跨模型、重复采样的行为基线，也无法区分任务能力失败与 provider/model 请求不兼容。首轮 pilot 中 GLM 5.2 与 DeepSeek V4 Flash 均通过 5/5，而 Qwen 3.6 35B 在首个请求即因 system message 协议兼容性失败，说明 matrix 必须把原始样本、聚合统计、基础设施失败和组合级停止策略作为同一套契约设计。

## What Changes

- 扩展 Eval CLI，使一次调用可以选择多个 task、多个 provider/model 组合，并为每个组合重复运行指定次数；现有单 task 用法保持兼容。
- 为 matrix 运行增加有界并发、稳定 run identity 和确定性输出顺序，避免并发结果污染不同临时 workspace/session。
- 增加组合级 preflight/熔断：若某 provider/model 在任务行为发生前出现确定性的请求兼容或配置失败，后续样本标记为 skipped，不再重复消耗整套任务预算。
- 细分 matrix failure taxonomy，至少区分 task validation、agent behavior/assertion、provider/config/infrastructure、timeout 与 internal harness failure，防止把模型不可运行误计为任务失败。
- 保留每次运行的原始 report，并生成聚合报告：按 task、provider/model 和整体汇总通过数/总数、失败分类、耗时、turn、token/cache 与 ContextDecision；统计结果必须展示样本数，避免把一次 pilot 当成稳定成功率。
- 文本输出面向本地阅读，JSON 输出提供稳定的 matrix report 对象，便于后续研究报告和 CI 消费。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `agent-eval`: 从单 task 评测扩展为兼容单次运行的 task/model matrix、重复采样、组合熔断以及原始与聚合报告契约。

## Impact

- 主要影响 `internal/eval` 的运行编排、failure taxonomy、report 数据模型与渲染。
- 影响 `internal/command/eval.go` 的 CLI 参数和退出状态语义。
- 更新 `openspec/specs/agent-eval`、requirements/design/roadmap 与 `docs/eval-tasks/README.md`。
- 不改变 task schema v1、单次 `Run` 的隔离边界、provider 配置加载或普通 Agent 行为。
