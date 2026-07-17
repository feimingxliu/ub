## Context

现有 headless `ub run` 已经负责 provider/model 解析、完整工具注册、权限、rollout、ContextWindowResolver 和 summary；rollout 也已经保存 tool result、usage、summary/context maintenance 与 assistant message。Eval 不应复制这些路径，否则评测到的是另一个运行时。另一方面，评测必须隔离 workspace 与 session state，并允许对真实 provider 运行，所以也不适合只扩充 fake-provider 单测。

## Goals / Non-Goals

**Goals:**

- 用声明式 YAML 表达 prompt、fixture 和可自动判定的结果。
- 对真实 `ub run` 做端到端评测，并从现有 rollout 提取工具、token/cache、turn 和 ContextDecision 指标。
- 默认隔离文件与 XDG state，失败时可保留现场。
- 让 task loader、runner、assertion evaluator 和 report renderer 可分别单测。

**Non-Goals:**

- 本切片不实现并行任务、批量模型矩阵、排行榜、SWE-bench adapter 或统计显著性分析。
- 不给 eval 引入专用 agent loop、provider 协议或另一套 rollout 格式。
- 不保证不同模型每次得到相同结果；MVP 只保证任务和判定过程可重复。
- 不自动下载 fixture、依赖或远程 task。

## Decisions

### 1. 以子进程运行当前可执行文件

`ub eval` 解析并准备任务后，以当前 executable 启动 `ub --mode full-access run -p ...`，传递 task 的 provider/model 覆盖。task 可包含 follow-up prompts；首轮结束后 Eval 从隔离 store 取得 session ID，并通过隐藏的 `ub run --session` 在同一会话上继续。子进程 cwd 指向临时 workspace，`XDG_STATE_HOME` 和 `XDG_DATA_HOME` 分别指向该次运行的隔离 state/data；用户的全局 provider 配置仍按正常规则读取。

这比在 command 包内直接调用 `runAgent` 更好：cwd/env/state 都是进程级状态，子进程天然隔离，也能覆盖真实 CLI 装配。runner 通过可注入的 process executor 单测，不在普通测试中调用真实模型。

### 2. YAML task 与路径安全

task schema v1 包含 `name`、`description`、首个 `prompt`、可选 `followup_prompts`、`fixture`、`timeout` 和 `assertions`。断言分为文件、命令和 rollout 三类：文件存在/正文，命令退出码/输出，工具调用/禁止调用/调用顺序/任一工具、assistant 文本和 context action。

`--task` 接受 YAML 路径，或把安全的短名称解析为当前 workspace 的 `docs/eval-tasks/<name>.yaml`。fixture 和文件断言路径必须是 task 文件相对路径或 workspace 相对路径，拒绝绝对路径、`..` 逃逸和 fixture symlink；命令断言是用户显式选择 task 后的受信本地 argv，不做 shell 语义改写，并继承该次 Eval 的隔离 XDG 环境。

### 3. 复用隔离 rollout 做判定与计量

子进程结束后，从隔离 session store 选择该次创建的唯一 session，并读取 rollout：

- `tool_result` 生成工具调用序列与错误信息；
- `usage` 累加 input/output/reasoning/cache read/cache write token；
- `summary` 提取 ContextDecision action/reason；
- user/assistant event 计算 turn 与最终回答。

命令失败与断言失败分开分类。报告固定包含 task、pass、failure category、duration、turns、tokens、tool calls、context decisions、assertion results，以及保留现场时的 workspace。

### 4. 首批任务保持小而可解释

在 `docs/eval-tasks/` 提供五个 v1 YAML 任务及最小 fixture，覆盖 roadmap 指定的工具选择、先读后改、失败验证不报成功、复杂任务更新 todo/plan、compact 后续接。任务可以依赖真实模型，因此默认测试只验证 schema、fixture 和断言结构，不自动执行它们。

### 5. CLI 退出码与输出

成功且全部断言通过时退出 0；agent 子进程失败、task/fixture 无效或任一断言失败时返回错误并退出非 0。默认文本报告面向人阅读；`--json` 在 stdout 输出单个稳定 JSON 对象，诊断写 stderr，方便后续批量 runner 消费。

## Risks / Trade-offs

- [模型输出具有随机性] → 首批断言优先检查文件、命令和 rollout 结构，不依赖长文本逐字匹配。
- [full-access 会执行 task 中的代码] → 每次使用临时 workspace，task 必须由用户显式选择，文档明确只运行受信 task。
- [全局 provider 配置可能引用相对文件或 hooks] → cwd 使用 fixture 是有意的真实运行语义；state 单独隔离，报告保留子进程 stderr。
- [上下文压缩任务难以跨模型稳定触发] → task 显式提供局部模型窗口/context 配置 fixture，并同时要求最终文件结果和 context action；不把单一自然语言回答作为通过条件。
- [通过子进程增加启动时间] → MVP 优先隔离与真实性；批量复用进程留待有数据后评估。
