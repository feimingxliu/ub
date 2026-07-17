# agent-eval Specification

## Purpose

定义声明式 coding-agent 行为任务、隔离执行、自动断言、rollout 指标和 Eval CLI 的稳定契约。

## Requirements

### Requirement: 声明式 Eval Task
系统 SHALL 支持 schema version 1 的 YAML eval task，至少表达名称、首个 prompt、可选 follow-up prompts/fixture/timeout，以及文件、命令和 rollout 断言。系统 MUST 在启动 agent 前拒绝未知 schema、缺失必填字段、空 follow-up prompt、绝对路径、路径逃逸和 fixture symlink。

#### Scenario: 加载命名任务
- **WHEN** 用户传入安全短名称且当前 workspace 的 `docs/eval-tasks/<name>.yaml` 存在
- **THEN** 系统 MUST 加载该任务并以 task 文件目录解析 fixture

#### Scenario: 拒绝逃逸 fixture
- **WHEN** task 的 fixture 或文件路径是绝对路径、包含 `..` 逃逸或经过 symlink
- **THEN** 系统 MUST 在启动 provider 或执行命令前返回可读错误

### Requirement: 隔离执行真实 Agent
系统 SHALL 在新建临时 workspace 中复制 fixture，并以当前 ub executable、task prompts、provider/model 覆盖和 full-access 模式执行 headless agent；存在 follow-up prompts 时 MUST 复用首轮创建的隔离 session 顺序执行。系统 MUST 为该次运行设置独立 `XDG_STATE_HOME` 与 `XDG_DATA_HOME`，不得写入调用者 workspace 或常规 session store。

#### Scenario: 成功运行 task
- **WHEN** task 有效且 agent 子进程成功完成
- **THEN** 系统 MUST 从临时 workspace 和隔离 rollout 计算断言与指标

#### Scenario: Agent 执行失败
- **WHEN** provider、agent 或工具导致子进程非零退出
- **THEN** 系统 MUST 把结果分类为 agent failure、保留可诊断 stderr 摘要并返回非零退出

### Requirement: 可组合断言
系统 SHALL 支持文件存在/包含/不包含、命令退出码/输出、工具必须调用/禁止调用/固定顺序/任一候选顺序/任一调用、assistant 包含/不包含和 ContextDecision action 断言。每条断言 MUST 产生独立的名称、通过状态和失败原因；所有断言通过时 task 才通过。

#### Scenario: 文件与 rollout 均符合预期
- **WHEN** agent 修改后的文件满足内容断言且 rollout 工具序列满足行为断言
- **THEN** 系统 MUST 将每条断言和整个 task 标记为通过

#### Scenario: 验证命令失败
- **WHEN** task 声明的验证命令退出码或输出不符合预期
- **THEN** 系统 MUST 将对应断言标记失败并把 task 分类为 assertion failure

### Requirement: Eval 指标与报告
系统 SHALL 从隔离 rollout 汇总 turn 数、input/output/reasoning/cache token、工具调用序列和 ContextDecision action/reason，并记录总耗时。默认输出 MUST 是简洁文本报告，`--json` MUST 输出单个机器可读结果对象且不得混入 agent stdout。

#### Scenario: 输出 JSON 报告
- **WHEN** 用户运行 `ub eval --task <task> --json`
- **THEN** stdout MUST 只包含一个可解析 JSON 对象，并包含 task、passed、failure_category、metrics 和 assertions

#### Scenario: 断言失败的退出状态
- **WHEN** agent 成功完成但至少一条断言失败
- **THEN** 命令 MUST 输出完整报告并以非零状态退出

### Requirement: Eval CLI
CLI SHALL 提供 `ub eval --task <name-or-path>`，支持可选 `--provider`、`--model`、`--timeout`、`--json` 和 `--keep-workspace`。`--timeout` MUST 覆盖 task timeout；`--keep-workspace` MUST 在成功或失败后保留现场并在报告中给出路径。

#### Scenario: 缺少 task
- **WHEN** 用户运行 `ub eval` 而未提供 `--task`
- **THEN** CLI MUST 返回明确的必填参数错误且不得启动 agent

#### Scenario: 保留评测现场
- **WHEN** 用户使用 `--keep-workspace`
- **THEN** 系统 MUST 不删除临时 workspace/state，并在文本和 JSON 报告中返回其绝对路径

### Requirement: 首批行为任务
仓库 SHALL 提供五个可加载的 Eval MVP task，分别覆盖合适的源码定位工具、修改前读取、失败验证不谎报成功、复杂任务使用 todo/plan、以及发生 compact 后继续完成任务。每个 task MUST 使用最小本地 fixture 和自动断言，不得要求人工判定。

#### Scenario: 校验内置任务集合
- **WHEN** 测试枚举 `docs/eval-tasks/` 中的 MVP task
- **THEN** 系统 MUST 成功解析五个唯一任务，并确认每个任务至少包含一条自动断言
