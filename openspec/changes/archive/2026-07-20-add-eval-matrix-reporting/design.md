## Context

当前 `internal/eval.Run` 负责一次 task 的临时 workspace/state/data 隔离、真实 Agent 子进程、rollout 观察、断言和单次报告；`internal/command/eval.go` 只接受一个 `--task`、一个 provider/model。首轮手工 pilot 需要外层逐个启动命令，GLM 5.2 与 DeepSeek V4 Flash 各通过 5/5，但 Qwen 3.6 35B 在第一个请求即返回 `System message must be at the beginning`。如果简单生成 task × model × repeat 笛卡尔积，这类确定性不兼容会重复消耗时间和请求预算，并被错误计入任务成功率。

实现必须复用现有单次 `Run`，不能复制 Agent loop，也不能让并发样本共享 workspace/session。现有单任务 CLI/JSON report 已是稳定接口，matrix 扩展需要保持向后兼容。

## Goals / Non-Goals

**Goals:**

- 一次命令运行多个 task、显式 provider/model target 和重复样本。
- 每个样本继续使用现有 `Run` 的完全隔离边界。
- 支持有界并发和确定性结果顺序。
- 在 target 首个样本出现前置基础设施失败时熔断剩余样本。
- 同时保留原始 report 和按整体/target/task 聚合的统计。
- 保持单 task、单 target、repeat=1 的旧输出与退出语义。

**Non-Goals:**

- 不实现分布式 worker、远程队列、数据库结果仓库或 Web dashboard。
- 不引入统计显著性判断；只报告样本数、比例和描述性汇总。
- 不自动枚举用户配置中的所有 provider/model，也不猜测 target 配对。
- 不把 assertion failure、模型行为错误或普通 tool failure 当作 target 熔断条件。
- 不修复 pilot 暴露的 Qwen/provider 请求兼容问题。

## Decisions

### 1. CLI 以重复 `--task` 和显式 `--target provider=model` 表达 matrix

`--task` 改为可重复参数；新增可重复 `--target <provider>=<model>`、`--repeat` 和 `--parallel`。target 使用第一个 `=` 分隔，允许 model ID 自带 `/`。`--target` 与旧 `--provider`/`--model` 互斥；未传 target 时仍从旧 flags/有效配置形成单一 target。

当解析后只有一个 task、一个 target、repeat=1 且没有显式 `--target` 时，继续调用旧单次路径并输出 `Report`。其余情况输出 `MatrixReport`。相比自动对 provider/model 列表做交叉积，显式 target 不会制造无效配对；相比新建 `eval matrix` 子命令，可保留 task 解析、timeout、JSON 和 workspace flags 的同一入口。

### 2. Matrix orchestration 组合现有 `Run`，不进入 Agent 内部

新增 `RunMatrix(ctx, MatrixRequest)`。请求包含已加载的 `TaskFile`、有序 target、repeat、parallel 和单次 `RunOptions` 模板。每个计划样本获得稳定 index/run ID，调用现有 `Run` 创建独立临时目录。生产路径使用默认 runner；测试可注入 `RunFunc`，避免真实 provider 和不稳定计时。

计划顺序固定为 target → task → repetition，最终结果始终按 index 排序，与 goroutine 完成顺序无关。worker pool 上限由 `--parallel` 控制，默认 1，拒绝非正数和过大的值。

### 3. 每个 target 的第一个样本兼作 preflight，并实施组合级熔断

每个 target 先独立运行计划中的第一个样本。只有当该样本分类为 `infrastructure`，且没有观察到 tool call 或 assistant 行为时，target 才进入熔断状态；其余计划样本生成 `skipped` 记录，引用触发熔断的 failure 摘要，不调用 `Run`。

单次 runner 在子进程失败后 best-effort 读取已产生的 rollout；对于请求建立前/首请求的 provider 配置、认证、连接和 4xx/协议错误，分类为 `infrastructure`。无法可靠判断时保留 `agent`，宁可多跑也不误熔断模型行为。preflight 本身是正式样本，不额外重复调用。

### 4. 原始结果与聚合结果同属一个稳定 MatrixReport

`MatrixReport` 包含 matrix 参数、按计划顺序排列的 `runs`、`overall`、`by_target` 和 `by_task`。每个 run 标记 run ID、task、target、repetition、status=`passed|failed|skipped`，执行过的样本携带完整 `Report`。

聚合至少包含 planned/executed/passed/failed/skipped、按 failure category 计数、pass rate（分母仅 executed）、duration/turn 平均值、input/output/reasoning/cache token 总量，以及 ContextDecision action 计数。所有比率必须同时输出分子/分母，样本为零时不产生 NaN/Inf。文本报告以汇总表和失败/熔断摘要为主；JSON 输出完整对象。

### 5. 退出状态与取消语义

所有计划样本通过时退出 0；任一 failed 或 infrastructure-triggered skipped 时输出完整 matrix report 后退出非零。调用 context 取消后，不再启动新样本，未开始项标为 skipped/canceled；已运行项保留结果。`--keep-workspace` 继续逐样本生效。

## Risks / Trade-offs

- **[基础设施错误分类依赖 provider 错误文本]** → 分类器使用保守的已知结构/关键词和“无工具、无 assistant 行为”双重条件；未知错误不熔断。
- **[并发请求触发 provider rate limit]** → 默认 parallel=1、设置小的硬上限，并把 rate-limit 作为样本基础设施失败展示而不是静默重试整个 matrix。
- **[完整 raw reports 导致 JSON 较大]** → MVP 优先可审计性；后续真实规模需要时再增加外部 JSONL/artifact 输出。
- **[单次与 matrix 两种 JSON shape 增加消费者分支]** → 旧单次 shape 完全不变；matrix 顶层使用明确 `kind: matrix` 和独立字段。
- **[preflight 任务本身偶然失败]** → 只有无 agent 行为的 infrastructure 分类触发熔断，assertion/agent failure 不触发。

## Migration Plan

1. 扩展单次失败观察与 infrastructure 分类，不改变已存在的分类值语义。
2. 新增 matrix 类型、planner、runner、aggregator 和渲染测试。
3. 扩展 CLI flags，并保留原单次分支测试。
4. 同步 specs/docs，使用 fake runner 做确定性 smoke；真实 provider pilot 不作为默认测试。

回滚时可移除 matrix flags/类型而保留增强后的单次诊断；已有 task 和单次调用无需迁移。

## Open Questions

无。JSONL 外部落盘、置信区间、基线对比和 CI threshold 留给后续真实使用数据决定。
