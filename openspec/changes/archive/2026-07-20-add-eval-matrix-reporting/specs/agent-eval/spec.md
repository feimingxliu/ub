## ADDED Requirements

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

## MODIFIED Requirements

### Requirement: Eval CLI
CLI SHALL 提供 `ub eval --task <name-or-path>`，`--task` MAY 重复；支持可选 `--provider`、`--model`、可重复 `--target <provider>=<model>`、`--repeat`、`--parallel`、`--timeout`、`--json` 和 `--keep-workspace`。`--target` MUST 与 `--provider`/`--model` 互斥。`--timeout` MUST 覆盖 task timeout；`--keep-workspace` MUST 对每个执行样本保留现场并在对应原始 report 中给出路径。解析后仅有一个 task、一个非显式 target 且 repeat=1 时，CLI MUST 保留原单次 Report 文本/JSON 和退出语义；其余情况 MUST 使用 MatrixReport。

#### Scenario: 缺少 task
- **WHEN** 用户运行 `ub eval` 而未提供任何 `--task`
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
