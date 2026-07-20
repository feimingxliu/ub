## Why

当前 Eval MVP 能隔离 workspace、state 和 rollout，但 task 无法控制 agent 的有效上下文窗口与压缩参数；`compact-continuation` 因而依赖所选模型的原生窗口大小，可能在没有触发 compact 时产生与模型行为无关的假失败。跨模型 pilot 之前需要先让这类运行时前置条件可声明、可审计且可重复。

## What Changes

- 为 schema version 1 eval task 增加受限的 task-local runtime overrides，首批覆盖 `max_context_tokens`、`context.trigger_ratio` 与 `context.keep_recent_turns`。
- Eval runner 为每次运行生成隔离配置层，在保留用户 provider 凭据和 provider/model 选择的同时，仅覆盖允许的运行时字段。
- 报告记录本次实际应用的 runtime overrides，便于比较和复现实验。
- 更新 `compact-continuation`，显式配置足以确定性触发压缩的上下文边界，不再依赖模型原生窗口。
- 移除没有运行时消费者的 `UB_EVAL` 环境变量，避免形成隐式且未经约束的评测模式。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `agent-eval`: eval task 可以声明受限的运行时覆盖；runner 必须隔离应用并在报告中公开有效覆盖，内置 compact task 必须确定性建立压缩前置条件。

## Impact

- 影响 `internal/eval` 的 task schema、校验、runner、report 和测试。
- 影响 `docs/eval-tasks/compact-continuation.yaml` 及 eval 使用文档。
- 更新 `agent-eval` 主规格以及 requirements/design/roadmap 中的 Eval 契约；不改变普通 `ub run` 的配置语义。
