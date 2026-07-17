## Why

ub 的 prompt、上下文决策和 provider cache 已经具备可观察基础，但当前仍缺少可重复运行的行为评测入口，后续优化只能依赖单测或人工体验。现在需要先建立一个小而稳定的 Eval MVP，用真实 `ub run`、隔离 workspace 和 rollout 断言量化“模型是否稳定做对”。

## What Changes

- 新增 `ub eval --task <name-or-path> [--provider/--model]`，运行单个声明式评测任务。
- 每次评测在临时 workspace 和隔离 state root 中执行当前 `ub run`，不污染调用者的项目文件或常规 session 数据。
- 支持 fixture、文件内容、命令退出码和 rollout 行为断言，并输出通过状态、turn、token/cache、工具调用、上下文维护和失败分类。
- 支持人类可读报告与 `--json` 机器可读报告，以及失败时保留 workspace 供诊断。
- 交付首批五个任务，覆盖工具选择、先读后改、验证诚实性、todo/plan 使用和 compact 后任务续接。

## Capabilities

### New Capabilities

- `agent-eval`: 声明式 coding-agent 行为任务、隔离执行、rollout/文件断言和评测报告。

### Modified Capabilities

无。`ub eval` 的 CLI 契约作为新 `agent-eval` 能力的一部分定义。

## Impact

- 新增 `internal/eval` 领域包和 `internal/command/eval.go` CLI 接入。
- 新增 `docs/eval-tasks/` 内置任务与 fixture，并更新 requirements/design/roadmap/usage。
- 复用现有 headless `ub run`、XDG state、SQLite rollout 和 provider 配置；不引入新的第三方依赖或 provider 协议。
