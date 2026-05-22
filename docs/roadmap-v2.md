# ub V2 演进路线图

> 状态:草案 (v0.2 起点 = v0.1.1 已发布)。本路线图覆盖 v0.2 ~ v1.0 期间的演进候选。
> 与 V1 路线图 ([`roadmap.md`](roadmap.md)) 不同,V2 不再走"每个迭代 = 一次 AI 会话"的强约束节奏 —— V1 把端到端打通,V2 是在打通基础上的深度演进与方向选择。

---

## 演进约束

ub 是**学习/研究向通用编程助手**(`requirements.md` §1)。V2 阶段所有演进必须回答这三个问题之一:

1. 是否让 ub **更易读、更适合被 fork**?
2. 是否让 ub **承载某个值得研究的命题**(架构模式、agent 设计、上下文管理实验)?
3. 是否解决**实际用户痛点**(已被 issue / 真实使用反馈印证)?

满足任一项才考虑做。**不为补齐和 Claude Code / OpenCode 的功能差距而做**。

---

## 路线图地图

| 方向 | 关注点 | 优先级 | 启动门槛 |
|---|---|---|---|
| §1 V1 收尾债务 | 工程债务、平台覆盖、社区基建 | **高(v0.2 必做)** | 立即 |
| §2 UX 小步快跑 | 零散小改进、good-first-issue 储备 | 中 | 立即,可与 §1 并行 |
| §3 核心能力扩展 | hooks / 记忆 / subagents / plan-execute | 中-高(V2 主体) | v0.2 完成后 |
| §4 架构优化 | 解耦、事件总线、tracing、provider 升级 | 学习高,产品低 | 任意 |
| §5 生态拓展 | IDE / Web / 协作 | 低(看社区信号) | ub 被实际采用之后 |
| §6 研究产出 | 博客、跨 provider 评测、内部文档系列 | 持续 | 立即,与代码工作并行 |

---

## §1 V1 收尾债务

**目标版本: v0.2.0。** 都是"开源后被新用户/贡献者看见的不专业信号",优先扫掉。

### S1-01 workspace 路径规范化
- **现状**:session 按 `os.Getwd()` 字符串严格隔离。symlink、`/proj` vs `/proj/sub` 视作不同 workspace
- **改动**:`internal/cli/tui.go` / `internal/cli/root.go` 写入 / 查询 session 时统一过 `filepath.Abs` + `filepath.EvalSymlinks` + `filepath.Clean`
- **可选增强**:向上找 `.git` 根目录,让子目录归并到 git 根
- **验证**:单测 + 手测:`/proj` 和 `/proj/sub` 启动 ub 看到同一组 session
- **工程量**: S

### S1-02 CONTRIBUTING.md
- 把 `AGENTS.md` 中对外部贡献者有用的部分(commit 风格、分支约定、测试要求、PR 模板)拆出独立的 `CONTRIBUTING.md`
- AGENTS.md 留 AI 协作专用约定
- **工程量**: XS

### S1-03 CHANGELOG.md + 自动化
- 用 `git-cliff` 或类似工具,从 Conventional Commits 自动生成
- GoReleaser 配置 release notes 抽取 changelog 对应章节
- **工程量**: S

### S1-04 Issue / PR 模板
- `.github/ISSUE_TEMPLATE/`:bug_report / feature_request / docs_feedback
- `.github/PULL_REQUEST_TEMPLATE.md`:勾选项(测试已加 / 文档已更新 / Breaking change)
- **工程量**: XS

### S1-05 Windows / WSL 真实验证
- 当前 `docs/install.md` 写了 Windows 步骤,但未实际验证
- 在 Windows 上跑 `make build / test / vet`、装 release archive、`ub` TUI 启动并交互一次
- 至少修一个 path / process group / 终端兼容性问题(预期会有)
- **工程量**: M

### S1-06 黑名单 fuzz 测试
- `internal/permission` 的硬编码黑名单(`rm -rf /` 类)目前用正则匹配。加 fuzz 测试覆盖:多空格、反斜杠转义、`$HOME` 变量展开、UTF-8 同形字符等
- **工程量**: S

### S1-07 MCP server 连接生命周期
- 当前 MCP server 启动失败 fail-open;断开后不会自动重连
- 加重连 backoff + 状态汇报(`ub doctor` 能看到 mcp server 当前是否连着)
- **工程量**: M

### S1-08 release 自动化扫尾
- 加 SBOM 生成(`syft` + GoReleaser)
- 加二进制签名(`cosign` keyless)
- README 加 verification 步骤
- **工程量**: M
- **可选**:看是否需要

---

## §2 UX 小步快跑

**目标**:零散小改进储备,可随时插入 sprint;好的 "good first issue" 起点。

| ID | 描述 | 工程量 |
|---|---|---|
| S2-01 | `/sessions` picker 加 fuzzy filter 输入框 | S |
| S2-02 | `ub sessions ls --all`:跨 workspace 列出所有 session,按 cwd 分组 | S |
| S2-03 | `/retry`:重跑上一次 user turn(不用复制 prompt) | S |
| S2-04 | TUI 内 `/doctor` slash 命令:不退出 TUI 跑健康检查 | S |
| S2-05 | Toast 通知层:tool 成功 / 失败 / 审批通过的瞬时反馈 | M |
| S2-06 | 状态栏 `?` 角标:点开/快捷键打开 cheatsheet | S |
| S2-07 | `/copy <N>`:复制第 N 条消息内容到系统剪贴板 | S |
| S2-08 | `/sessions search <query>`:跨 session 全文搜索 rollout 内容 | M |
| S2-09 | TUI 启动时检测终端宽度过窄(< 80 列)给提示 | XS |
| S2-10 | `ub doctor --json`:机器可读输出,便于 CI 集成 | S |

---

## §3 核心能力扩展

**目标**:每条都是一次完整的"学习单元",可单独写博客 / 论文 / 内部文档。

### S3-01 Hooks 机制

灵感来自 Claude Code 的 `hooks` 配置。

- **场景**:`pre_tool_call` / `post_tool_call` / `pre_user_turn` / `post_user_turn` 触发 shell 命令
- **典型用例**:每次 edit 后自动 gofmt;每次 bash 前打 audit log;commit 前跑 lint
- **配置**: `~/.config/ub/hooks.yaml`(或 workspace `.ub/hooks.yaml`)
- **设计要点**:
  - hook 进程隔离(超时、stdout/stderr 限幅、env 白名单)
  - hook 失败是否阻断 tool 调用(可配)
  - hook 上下文(tool name / args / result)以 env 或 stdin JSON 注入
- **学习价值**:事件驱动、用户脚本沙箱、shell 集成
- **工程量**: M

### S3-02 Workspace 持久记忆

灵感来自 Claude Code 的 `CLAUDE.md` + auto-memory。

- **场景**:agent 在多次会话中累积"build 命令是 X / 测试方式是 Y / 仓库代码风格 Z"等
- **存储**: `.ub/memory.md`(workspace 级)+ `~/.config/ub/memory.md`(全局)
- **写入策略**:
  - 显式:`/remember <text>`
  - 自动:agent 完成某 turn 后,small_model 决定是否归纳一条到 memory
  - rollout 中可见:每次写入对应一个 `MemoryWrite` 事件
- **读取**:每次发请求前注入到 system prompt(可配 max chars)
- **难点**:冲突合并、衰减、隐私(避免泄露临时调试信息)
- **学习价值**:长期记忆 / RAG / 记忆生命周期管理
- **工程量**: L

### S3-03 Subagents(多 agent 协作)

- **场景**:主 agent 派发独立 context 的子 agent 做调研 / 子任务,结果汇总回主流程
- **最简版**:`task("explore module X and report")` 工具,创建子 session,跑完返回最终消息
- **完整版**:子 agent 有独立 model / tools / mode 配置
- **UI**:TUI 中并列显示多个 agent 状态(类似 tmux 多 pane)
- **学习价值**:多 agent 编排、context isolation、token 经济、协作模式
- **工程量**: L
- **依赖**: §4.1 agent loop 解耦先做

### S3-04 Plan-then-execute

把 `plan` 模式从"工具拦截"升级到"plan artifact"工作流。

- **plan 阶段**:模型输出 `.ub/plans/<id>.md`,列出步骤、风险、回滚策略
- **review**:user 在 TUI 内打开 plan markdown,可编辑
- **execute 阶段**:切到 `work` 模式,agent 按 plan 一步步执行,每步对照 plan 检查
- **可中断**:任意步骤中断后下次能从断点继续
- **学习价值**:长任务编排、可恢复 agent、self-check
- **工程量**: M

### S3-05 multiedit 工具

- 设计 (`docs/design.md`) 中已声明,实际未实现
- **场景**:同一个 tool call 做多个文件的多处编辑,减少 round trip
- **schema**:`{edits: [{path, old, new, replace_all?}]}`
- **实现要点**:原子性(任一失败回滚)、preview 合并 diff 列表
- **工程量**: S

### S3-06 Tool I/O streaming

- **现状**:tool 同步阻塞,长 tool(`go test ./...`)期间 TUI 死等
- **目标**:tool 通过 channel 流式回传 partial result,TUI 实时显示
- **改动面**:tool 接口扩 `Run(ctx, args, events chan<- Event)`、agent loop 适配、TUI 处理 partial 渲染
- **学习价值**:流式 IO、channel 设计、cancellation 语义
- **工程量**: L
- **风险**:tool 接口 breaking change,需要小心兼容

### S3-07 LSP 工具扩充

- 当前只暴露 `diagnostics` / `references`
- 补:`hover`、`completion`、`document_symbols`、`rename`、`code_action`
- **价值**:agent 能做更细粒度代码操作
- **工程量**: M

### S3-08 Tool-result snapshot 工具

- agent 经常想看上一次 tool 的输出做 follow-up,但 tool result 被压缩
- 提供 `tool_result(turn_id)` 工具:从 rollout 拉回某次 tool 的原始输出(从 spillover 文件)
- **学习价值**:event log 的反向消费
- **工程量**: S

---

## §4 架构优化

**目标**:每条都让 ub 比当前更易读、更易 fork、更适合做研究脚手架。

### S4-01 Agent loop 解耦

现状:`internal/agent/loop.go` 把 prompt build / tool dispatch / context mgmt / summary 混在一起。

拆为五个独立子包:
- `agent/prompt`:纯函数,接收 history + tools + ctx + memory,产出 messages
- `agent/dispatch`:从 provider event 提取 tool calls
- `agent/exec`:并发执行 tool(集成 permission)
- `agent/ctx`:估算 token,决定是否触发 compaction
- `agent/loop`:协调以上四块的 orchestrator

**价值**:贡献者可以单独替换其中一块做实验(reflexion loop / tree-of-thought / lookahead planner...)。

**工程量**: M-L
**测试**:每个子包独立 unit test;loop 用 fake 拼装

### S4-02 事件总线

现状:rollout / TUI / spillover / log / 未来的 webhook 各自订阅 agent 事件,代码散落。

抽象 `internal/eventbus`:
- 一处发布,多处订阅(channel fan-out)
- 类型化 topic(`ToolCallStart` / `ToolCallEnd` / `Usage` / `ModeSwitch`...)
- 可挂载外部 webhook subscriber

**价值**:事件驱动架构 / pub-sub 教学样本

**工程量**: L

### S4-03 OpenTelemetry tracing

- 每个 turn / tool call / provider request 一个 span
- 本地用 Jaeger / honeycomb 看 agent 决策路径
- 副产物:"为什么这个 turn 花了 30 秒"分析
- **工程量**: M
- **学习价值**:分布式追踪 / 性能分析 / agent 调试

### S4-04 Provider 抽象升级

- **OpenAI Responses API**:多步 conversation,state in provider(避免重复发完整 history)
- **Anthropic memory tool**:让 provider 端管理 memory
- **结构化输出统一**:function calling / JSON mode / Anthropic tool 抽象成一个 interface
- **Thinking 内容统一**:OpenAI reasoning / Anthropic extended thinking / DeepSeek `reasoning_content` 抽象

**工程量**: M-L
**学习价值**:provider 适配层是观察 LLM 生态演化的好窗口

### S4-05 Sidecar tool process

把内置 tool 也走 MCP 协议(子进程 + stdio JSON-RPC),内置 / 外部 tool 用同一套接口。

- **价值**:协议统一、进程隔离、安全(tool crash 不影响主进程)
- **风险**:fork 进程开销、IPC 延迟、不稳定 tool 调试更难
- **工程量**: XL
- **推荐**:**先不做**,等 §4.2 事件总线和 §3.6 streaming 沉淀经验后再说

---

## §5 生态拓展(看社区信号决定)

**触发条件**:ub 真的被一定数量的用户在用(GitHub star > 500、社区 issue 活跃)再启动。

| ID | 项目 | 描述 |
|---|---|---|
| S5-01 | VS Code 扩展 | 通过 `ub run -p` 调本地 ub,结果展示在 IDE 面板 |
| S5-02 | Zed / Cursor 扩展 | 同上 |
| S5-03 | 本地 Web UI | `ub serve --port 8080`:browser chat,架构已支持(TUI 是其中一种 frontend) |
| S5-04 | 多人协作 session | WebSocket 共享 rollout 流,多人同 session |
| S5-05 | SWE-bench leaderboard | 让 ub 跑 [SWE-bench](https://www.swebench.com/) 子集,发性能报告 |

---

## §6 研究产出(持续)

工程进展之外应该产出**可被引用的内容**,这是学习/研究向项目的核心交付。

### S6-01 博客系列:ub 内部解析

每个核心模块写一篇 ~3000 字解析:

1. Agent loop in 200 lines(`internal/agent/loop.go` 拆解)
2. 把工具用权限包起来(`internal/permission`)
3. 流式 Bubble Tea + channel 模型(`internal/tui`)
4. 用 rollout 事件日志做 agent 调试(`internal/rollout`)
5. Token 估算与自动压缩(`internal/context`)
6. 用 fake provider 做确定性 agent 测试(`internal/provider/fake`)
7. tab 字符是怎么把我的 TUI 搞坏的 — 一个 wrap math 的 bug(我们刚跑完的修复,鲜活案例)

**节奏**:与代码工作并行,每完成一个 §3 / §4 项配一篇

### S6-02 跨 provider 行为对比

利用 rollout 数据:相同 prompt + 相同工具集 → 不同 model → 收集 tool-call pattern 差异。

- 比较维度:工具使用频次、首次工具调用 latency、错误恢复模式、token 经济
- 数据来源:一组标准任务(20-50 个)跑遍 Claude / GPT / DeepSeek / Qwen / Llama
- **价值**:LLM 评测生态稀缺这种 agent 行为对比的可复现数据

### S6-03 简单 eval 框架

- 内置一组小任务(`docs/eval-tasks/`):20-50 个 "改个 bug + 测试通过"、"按描述写一个文件"、"按规范重构" 等
- `ub eval --task X --model Y`:跑指定任务,rollout 自动对比预期结果
- 输出:成功率、平均 turn 数、平均 token 用量、失败模式分类
- **价值**:让 ub 自己作为评测脚手架

### S6-04 内部文档系列(docs/internals/)

教读者怎么拆开 ub 改某一块:

- `internals/adding-a-provider.md`:从零加一个 provider(以新假想的 X provider 为例)
- `internals/adding-a-tool.md`:Tool / PreviewableTool 接口拆解
- `internals/replacing-agent-loop.md`:把 loop.go 替换成 reflection-based agent
- `internals/rollout-format.md`:event JSON schema 精确定义

**节奏**:每次有 contributor 问"我想改 X"时就追加一篇

---

## 优先级与节奏建议

按"学习/研究向"原则,推荐如下节奏:

### v0.2.0 (4-6 周)
- 完成 §1 全部(8 项)
- 选 §2 中 2-3 项(推荐 S2-01 fuzzy filter、S2-02 全局 session 列表、S2-04 内置 `/doctor`)
- 启动 §6.1 博客系列第一篇(写 tab wrap bug 修复全过程)

### v0.3.0 (1-2 个月)
- 挑 §3 中 1-2 项(推荐 S3-05 multiedit + S3-01 hooks,工程量适中、用户价值明确)
- 同步启动 §4.1 agent loop 解耦(为后续 §3.3 subagents 铺路)
- §6.1 博客继续

### v0.4.0 (1-2 个月)
- 挑 §3 中 1-2 项(推荐 S3-04 plan-then-execute、S3-02 memory)
- §4.2 事件总线 或 §4.3 OTel tracing 二选一
- 启动 §6.3 eval 框架

### v0.5+ ~ v1.0
- §3.3 subagents(依赖 §4.1 完成)
- §3.6 tool streaming
- §4.4 provider 抽象升级
- §6.2 跨 provider 行为对比报告

### v1.0 之后
- §5 生态拓展(视社区信号)
- §4.5 sidecar tool process

---

## 跨阶段约定

### 提交规范
延续 V1:Conventional Commits 主体,`[V2-S<section>-NN] <summary>` 仅在明确实现本路线图某条目时使用。

### 文档同步
- 启动一个 §1-§6 条目前,先在 [`openspec/changes/`](../openspec/changes/) 开一个 change 文档讨论设计
- 完成后归档到 `openspec/changes/archive/`,并同步更新 [`requirements.md`](requirements.md) / [`design.md`](design.md) 中相关章节
- 涉及 user-facing 行为变化的,同步更新 [`usage.md`](usage.md)

### 测试约定
- 单测覆盖率不下降
- Tool / Provider / 权限系统改动必须 vcr 录回放
- 任何 UI 改动需要 `teatest` 单测覆盖关键路径

### 版本规则
- 0.X 阶段保留 breaking change 自由度,但每次 breaking change 必须在 CHANGELOG 显式列出
- 1.0 之后遵循 SemVer

---

## 开放问题

需要在执行前讨论清楚的设计抉择:

1. **Hooks 进程隔离强度**:用 OS sandbox(landlock / seccomp / sandbox-exec)还是仅靠超时 + env 白名单?
2. **Memory 的 LLM-driven 写入是否值得**:成本(每 turn 多一次 small_model 调用)vs 收益(用户感知到 agent "记住了")
3. **Subagents 的 UI 模型**:并列 pane vs 嵌套展开 vs 标签切换?
4. **eval 框架的任务集**:自建 vs 用 SWE-bench-lite 子集?
5. **Provider 抽象升级**:OpenAI Responses API 在 stateless 客户端语义下是否值得?ub 是否应该一直保持 stateless(便于 rollout)?

每个开放问题在对应条目启动前,通过 `openspec/changes/` 单开一个 change 讨论。

---

## 与 V1 路线图的关系

- [`roadmap.md`](roadmap.md) (V1)已 feature-complete,作为历史存档不再更新
- V1 的 35 个迭代(I-01 ~ I-35)对应今天 ub 的全部基础设施
- V2 是在 V1 基础设施之上的演进,不重复 V1 已经做过的事
- V1 的"垂直切片优先"、"测试即验收"等原则在 V2 继续生效
