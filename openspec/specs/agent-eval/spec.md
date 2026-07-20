# agent-eval Specification

## Purpose

定义声明式 coding-agent 行为任务、隔离执行、自动断言、rollout 指标和 Eval CLI 的稳定契约。

## Requirements

### Requirement: 声明式 Eval Task
系统 SHALL 支持 schema version 1 的 YAML eval task，至少表达名称、首个 prompt、可选 follow-up prompts/fixture/timeout、受限的 context runtime overrides，以及文件、命令和 rollout 断言。runtime overrides MUST 仅允许声明 `max_context_tokens`、`context.trigger_ratio` 和 `context.keep_recent_turns`，并在启动 agent 前完成范围校验。系统 MUST 在启动 agent 前拒绝未知 schema、缺失必填字段、无效 runtime override、空 follow-up prompt、绝对路径、路径逃逸和 fixture symlink。

#### Scenario: 加载命名任务
- **WHEN** 用户传入安全短名称且当前 workspace 的 `docs/eval-tasks/<name>.yaml` 存在
- **THEN** 系统 MUST 加载该任务并以 task 文件目录解析 fixture

#### Scenario: 拒绝逃逸 fixture
- **WHEN** task 的 fixture 或文件路径是绝对路径、包含 `..` 逃逸或经过 symlink
- **THEN** 系统 MUST 在启动 provider 或执行命令前返回可读错误

#### Scenario: 拒绝无效 runtime override
- **WHEN** task 声明非正数 `max_context_tokens`、范围外的 `context.trigger_ratio`、非正数 `context.keep_recent_turns` 或未知 runtime 字段
- **THEN** 系统 MUST 在启动 provider 前返回指出具体字段的 task validation error

### Requirement: 隔离执行真实 Agent
系统 SHALL 在新建临时 workspace 中复制 fixture，并以当前 ub executable、task prompts、provider/model 覆盖和 full-access 模式执行 headless agent；存在 follow-up prompts 时 MUST 复用首轮创建的隔离 session 顺序执行。系统 MUST 为该次运行设置独立 `XDG_STATE_HOME` 与 `XDG_DATA_HOME`，不得写入调用者 workspace 或常规 session store。task runtime overrides MUST 通过仅作用于该次 headless 子进程的显式参数应用，不得复制、重写或泄露用户 provider 凭据，也不得修改用户全局或 workspace 配置。

#### Scenario: 成功运行 task
- **WHEN** task 有效且 agent 子进程成功完成
- **THEN** 系统 MUST 从临时 workspace 和隔离 rollout 计算断言与指标

#### Scenario: Agent 执行失败
- **WHEN** provider、agent 或工具导致子进程非零退出
- **THEN** 系统 MUST 把结果分类为 agent failure、保留可诊断 stderr 摘要并返回非零退出

#### Scenario: 应用 context runtime overrides
- **WHEN** task 声明 context runtime overrides
- **THEN** 每个首轮及 follow-up 子进程 MUST 使用相同的规范化覆盖，且普通 `ub run` 和用户配置 MUST 保持不变

### Requirement: 可组合断言
系统 SHALL 支持文件存在/包含/不包含、命令退出码/输出、工具必须调用/禁止调用/固定顺序/任一候选顺序/任一调用、assistant 包含/不包含和 ContextDecision action 断言。每条断言 MUST 产生独立的名称、通过状态和失败原因；所有断言通过时 task 才通过。

#### Scenario: 文件与 rollout 均符合预期
- **WHEN** agent 修改后的文件满足内容断言且 rollout 工具序列满足行为断言
- **THEN** 系统 MUST 将每条断言和整个 task 标记为通过

#### Scenario: 验证命令失败
- **WHEN** task 声明的验证命令退出码或输出不符合预期
- **THEN** 系统 MUST 将对应断言标记失败并把 task 分类为 assertion failure

### Requirement: Eval 指标与报告
系统 SHALL 从隔离 rollout 汇总 turn 数、input/output/reasoning/cache token、工具调用序列和 ContextDecision action/reason，并记录总耗时。报告 MUST 包含 task 实际声明并应用的规范化 runtime overrides。默认输出 MUST 是简洁文本报告，`--json` MUST 输出单个机器可读结果对象且不得混入 agent stdout。

#### Scenario: 输出 JSON 报告
- **WHEN** 用户运行 `ub eval --task <task> --json`
- **THEN** stdout MUST 只包含一个可解析 JSON 对象，并包含 task、passed、failure_category、runtime、metrics 和 assertions

#### Scenario: 断言失败的退出状态
- **WHEN** agent 成功完成但至少一条断言失败
- **THEN** 命令 MUST 输出完整报告并以非零状态退出

### Requirement: Eval CLI
CLI SHALL 提供 `ub eval --task <name-or-path>`，`--task` MAY 重复；支持可选 `--provider`、`--model`、可重复 `--target <provider>=<model>`、`--repeat`、`--parallel`、`--timeout`、`--json` 和 `--keep-workspace`。`--target` MUST 与 `--provider`/`--model` 互斥。`--timeout` MUST 覆盖 task timeout；`--keep-workspace` MUST 对每个执行样本保留现场并在对应原始 report 中给出路径。解析后仅有一个 task、一个非显式 target 且 repeat=1 时，CLI MUST 保留原单次 Report 文本/JSON 和退出语义；其余情况 MUST 使用 MatrixReport。

#### Scenario: 缺少 task
- **WHEN** 用户运行 `ub eval` 而未提供 `--task`
- **THEN** CLI MUST 返回明确的必填参数错误且不得启动 agent

#### Scenario: 保留评测现场
- **WHEN** 用户使用 `--keep-workspace`
- **THEN** 系统 MUST 不删除任何已执行样本的临时 workspace/state，并在各自文本和 JSON 原始 report 中返回绝对路径

#### Scenario: 保持单次调用兼容
- **WHEN** 用户只传一个 task、至多一个旧 provider/model 覆盖且 repeat=1
- **THEN** CLI MUST 调用单次 runner，并保持既有 Report 输出结构和退出状态

#### Scenario: 解析显式 target
- **WHEN** 用户重复传入格式正确的 `--target provider=model`
- **THEN** CLI MUST 按参数顺序建立 target 列表，保留 model ID 中的 `/`，且不得自动生成 provider/model 交叉积

### Requirement: Eval Matrix 编排
系统 SHALL 通过组合多个 Eval task、显式 provider/model target 和正整数 repetition 生成稳定的样本计划，并以可配置的正整数并发上限调用现有隔离单次 runner。每个执行样本 MUST 拥有独立 workspace/state/data/session，最终结果顺序 MUST 与稳定计划顺序一致而不受完成顺序影响。

#### Scenario: 运行多任务多模型矩阵
- **WHEN** 用户选择两个 task、两个显式 target 和两次 repetition
- **THEN** 系统 MUST 计划八个具有唯一 run identity 的样本，在并发上限内执行并按 target、task、repetition 的稳定顺序返回

#### Scenario: 拒绝无效 matrix 参数
- **WHEN** repetition、parallel 非正数，parallel 超过实现上限，target 缺少 provider/model，或显式 target 与旧 provider/model flags 混用
- **THEN** CLI MUST 在启动任何 Agent 前返回可读参数错误

#### Scenario: 取消 matrix
- **WHEN** matrix context 在部分样本完成后被取消
- **THEN** 系统 MUST 停止启动新样本、保留已完成结果，并把未启动样本标记为 skipped/canceled

### Requirement: Target Preflight 与熔断
系统 SHALL 把每个 target 的第一个计划样本作为正式 preflight 样本。只有当该样本在没有 tool call 和 assistant 行为时分类为 provider/config/infrastructure failure，系统 MUST 熔断该 target，并将其余计划样本标记为 skipped；assertion failure、普通 agent failure 或已发生 agent 行为后的失败 MUST NOT 触发熔断。

#### Scenario: 请求协议不兼容触发熔断
- **WHEN** target 的首个样本在首次请求返回确定性 4xx/协议错误，且 rollout 没有 tool call 或 assistant 内容
- **THEN** 系统 MUST 保留该 infrastructure failure 原始 report，不执行该 target 的其余样本，并为它们生成引用根因的 skipped 记录

#### Scenario: 行为断言失败不触发熔断
- **WHEN** target 的首个样本完成 Agent 行为但 assertion 不通过
- **THEN** 系统 MUST 记录失败并继续执行该 target 的其余计划样本

### Requirement: Eval Matrix 聚合报告
系统 SHALL 在一个 MatrixReport 中保留每个执行样本的完整单次 Report 和每个 skipped 记录，并按 overall、target 和 task 生成描述性聚合。聚合 MUST 包含 planned/executed/passed/failed/skipped、failure category 计数、以 executed 为分母的 pass rate、耗时/turn 汇总、token/cache 总量和 ContextDecision action 计数，且 MUST 显式给出样本数。

#### Scenario: 输出机器可读 matrix JSON
- **WHEN** 用户对 matrix 运行使用 `--json`
- **THEN** stdout MUST 只包含一个带 `kind=matrix`、参数、runs、overall、by_target 和 by_task 的可解析 JSON 对象

#### Scenario: 零执行样本的安全聚合
- **WHEN** 某分组中的计划样本全部 skipped
- **THEN** 聚合 MUST 报告 executed=0、pass rate=0，且不得输出 NaN 或 Infinity

#### Scenario: Matrix 包含失败
- **WHEN** 任一执行样本 failed 或任一 target 因 infrastructure failure 产生 skipped 样本
- **THEN** CLI MUST 输出完整 matrix report 并以非零状态退出

### Requirement: 首批行为任务
仓库 SHALL 提供五个可加载的 Eval MVP task，分别覆盖合适的源码定位工具、修改前读取、失败验证不谎报成功、复杂任务使用 todo/plan、以及发生 compact 后继续完成任务。compact continuation task MUST 声明足以建立压缩前置条件的 runtime overrides，并以 rollout ContextDecision 断言确认实际发生 compact。每个 task MUST 使用最小本地 fixture 和自动断言，不得要求人工判定。

#### Scenario: 校验内置任务集合
- **WHEN** 测试枚举 `docs/eval-tasks/` 中的 MVP task
- **THEN** 系统 MUST 成功解析五个唯一任务，并确认每个任务至少包含一条自动断言

#### Scenario: compact task 建立确定性前置条件
- **WHEN** runner 执行内置 compact continuation task
- **THEN** runner MUST 把 task 声明的窗口和 context 参数应用到每个 turn，并且 task 只有在 rollout 记录 compact action 后才通过
