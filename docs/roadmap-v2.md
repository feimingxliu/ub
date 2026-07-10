# ub V2 演进路线图

> 状态:当前规划对齐 (2026-07-09,当前发布基线 = v0.4.3)。本路线图覆盖 v0.2 ~ v1.0 期间的已交付基线与后续演进候选。
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

| 方向 | 关注点 | 当前状态 | 下一步 |
|---|---|---|---|
| §1 V1 收尾债务 | 工程债务、平台覆盖、社区基建 | **v0.2.x 已完成** | 只做回归修复 |
| §2 UX 小步快跑 | slash 命令、会话恢复、输入/活动体验 | **v0.2.x ~ v0.4.x 大体完成** | 收敛真实使用反馈 |
| §3 核心能力扩展 | hooks / memory / subagents / plan-execute / web / goal | **主体已落地** | 做小范围语义补强 |
| §4 架构优化 | prompt / context / eventbus / tracing / artifact / provider cache | **当前主战场** | 优先上下文决策、prompt/cache 与可观测性 |
| §5 生态拓展 | extension / capability discovery / IDE / Web / 协作 | 未启动 | 先稳定内部扩展边界,再等社区采用信号 |
| §6 研究产出 | 博客、跨 provider 评测、eval、内部文档 | 滞后于代码 | 从 eval 与 internals 补起 |

## 当前基线快照

截至 v0.4.3,ub 已经具备一个完整的本地 coding agent 基线:

- V1 基础设施已完成并归档:`provider`、rollout、TUI、tool、权限、MCP、LSP、summary、resume 和 release 流程。
- V2 用户可见能力已大面积落地:hooks、workspace/global memory、`task` 子 agent、plan/todo 分离、`multiedit`、tool streaming、LSP 扩展、`tool_result`、长输出 spillover、prompt harness、`ask`、web tools、模型发起 plan mode、full-access、`/rewind`、`/btw`、goal mode。
- 当前主要缺口不再是"补功能",而是 ContextDecision/语义裁剪、prompt builder 后续分层、prefix cache、事件/trace 观测、eval 与研究输出；上下文窗口 resolver 已落地第一切片。
- 旧的 v0.2/v0.3/v0.4 节奏建议保留为历史参考;真正的后续版本从 `v0.5 Context & Prompt Reliability` 重新排队。

---

## §1 V1 收尾债务

**状态:已完成(v0.2.x)。** 本 section 现在作为历史基线保留;后续只在发现回归时修复。原目标是扫掉"开源后被新用户/贡献者看见的不专业信号"。

### S1-01 workspace 路径规范化
- **现状**:session 按 `os.Getwd()` 字符串严格隔离。symlink、`/proj` vs `/proj/sub` 视作不同 workspace
- **改动**:`internal/app/ub/cli/tui.go` / `internal/app/ub/cli/root.go` 写入 / 查询 session 时统一过 `filepath.Abs` + `filepath.EvalSymlinks` + `filepath.Clean`
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
- `internal/pkg/runtime/permission` 的硬编码黑名单(`rm -rf /` 类)目前用正则匹配。加 fuzz 测试覆盖:多空格、反斜杠转义、`$HOME` 变量展开、UTF-8 同形字符等
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

### S1-09 job_run 并发与生命周期收口
- **现状**:`job_run` / `job_output` / `job_kill` 没有并发上限,完成后也不会自动回收;ub 进程长跑时,完成的 job 会一直留在 manager map 里
- **目标**:对齐 Crush(`.references/crush/internal/shell/background.go`)的工程规范
  - 启动时检查 manager 当前 job 数,达到上限(默认 50,配置项 `tools.job.max_concurrent`)拒绝新任务并返回明确错误
  - 完成的 job 标记 `completedAt`,后台定时任务每 N 分钟扫一次,清理超过保留期(默认 8 小时,配置项 `tools.job.retention`)的条目
  - 增加 `Manager.Shutdown(ctx)`:`ub` 退出 / `Close()` 时统一 SIGTERM → SIGKILL 所有 running job,避免遗留孤儿进程依赖 OS reap
- **学习价值**:长跑服务的资源回收语义、`time.Ticker` + `sync.Map` 的清扫器模式
- **工程量**: S
- **测试**:并发上限 reject、过期 cleanup、shutdown 全终止三条 case

---

## §2 UX 小步快跑

**状态:主体已完成(v0.2.x ~ v0.4.x)。** 这个 section 后续不再按固定 backlog 推进,只承接真实使用中暴露的小体验问题。

| ID | 描述 | 状态 |
|---|---|---|
| S2-01 | `/sessions` picker 加 fuzzy filter 输入框 | 已实现 |
| S2-02 | `ub sessions ls --all`:跨 workspace 列出所有 session,按 cwd 分组 | 已实现 |
| S2-03 | `/retry`:重跑上一次 user turn(不用复制 prompt) | 已实现 |
| S2-04 | TUI 内 `/doctor` slash 命令:不退出 TUI 跑健康检查 | 已实现 |
| S2-05 | Toast 通知层:tool 成功 / 失败 / 审批通过的瞬时反馈 | 已实现 |
| S2-06 | 状态栏 `?` 角标:点开/快捷键打开 cheatsheet | 已实现 |
| S2-07 | `/copy <N>`:复制第 N 条消息内容到系统剪贴板 | 已实现 |
| S2-08 | `/sessions search <query>`:跨 session 全文搜索 rollout 内容 | 已实现 |
| S2-09 | TUI 启动时检测终端宽度过窄(< 80 列)给提示 | 已实现 |
| S2-10 | `ub doctor --json`:机器可读输出,便于 CI 集成 | 已实现 |
| S2-11 | `/rewind [turn]`:回退到指定历史 turn,默认回退上一轮 user turn | 已实现 |
| S2-12 | `/btw [question]`:旁路快问快答,不打断当前任务且不污染主对话历史 | 已实现 |

v0.4.x 额外吸收了一批真实使用反馈:启动速度、SQLite busy、compacting 状态、textarea 输入、ask 自定义回答和长问题换行、运行中 Enter/TAB 引导/排队、关键路径日志、内存分析、goal mode、`sessions ls` 工作区列等。这些不再单独拆 S2 编号,统一视为 UX 收敛。

新增候选说明:

- **S2-11 `/rewind`**:面向"刚才这轮跑偏了"的恢复入口。MVP 先基于 session/rollout event 回退对话上下文、TUI 显示和 context 估算到目标 user turn 之前;如果目标 checkpoint 与当前 workspace 相比存在已跟踪文件改动,先展示受影响文件并要求确认,再决定是否回退文件。不能静默丢弃当前 workspace 未提交改动;无法从 checkpoint 可靠恢复时只 rewind 对话并明确提示用户。
  - 已实现: `/rewind` 打开历史 user turn 选择器,默认按最近 turn 在前展示,支持筛选;选中某条 user message 后删除该 turn 及之后的 rollout events,重建 runner conversation history、TUI transcript 和 context 状态,并把选中的 user message 放回输入框供用户修改重发。文件回退参考 Claude 的 file history:每个 user turn 开始前记录 `file_history_snapshot`,把已跟踪文件旧内容备份到 state root;`write` / `edit` / `multiedit` 和字面路径 `rm` / `git rm` 形式的 `bash` 删除在执行前补齐当前 turn 的旧状态。若当前 workspace 与目标 checkpoint 不一致,会进入二次选择:默认只回退对话并保留 workspace 文件,也可显式选择同时把 checkpoint 中可恢复的文件回到目标消息之前的状态。不可靠或 workspace 外路径会跳过并在 TUI 提示,但对话 rewind 仍会完成。
- **S2-12 `/btw`**:对齐 Claude Code 的 side question 语义,面向"我想临时问一句,但不想打断主任务或污染上下文"。输入 `/btw <question>` 后启动一次独立的旁路模型请求,复用当前会话上下文和 prompt cache,但不允许工具访问、文件读取、命令执行或搜索;回答在独立 BTW TUI 视图内展示,问题和答案不写入主 conversation/history/context。运行中可用,不取消当前 Agent turn;旁路回答只在 BTW 视图内临时保留,继续追问时复用视图内 Q/A 作为临时 side history,不污染主 session。
  - 已实现: `/btw [question]` 打开独立的内存 BTW 视图;带 question 时通过独立 no-tool provider 请求回答,运行中立即执行且不进入主队列;请求只携带 text-only 主上下文,过滤 tool_use/tool_result,并把 provider tool call 或伪工具标记按错误处理;视图内支持多条 Q/A、继续追问、Markdown 渲染回答、独立滚动长输出、复制最新答案和清空记录;`Esc` 返回主对话并清空 BTW 临时历史;问题/答案不写入主 transcript、session history 或 rollout。

---

## §3 核心能力扩展

**目标**:每条都是一次完整的"学习单元",可单独写博客 / 论文 / 内部文档。
§3 关注用户能直接感知到的 agent 能力;如果某项主要是代码组织方式,放到 §4。

### S3-01 Hooks 机制

**状态**:已实现。当前支持 `pre_tool_call` / `post_tool_call` / `pre_user_turn` / `post_user_turn` 四个 lifecycle hook,配置并入主配置体系和 schema。

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

灵感来自通用 `AGENTS.md` 工作区指令 + auto-memory。

- **状态**:已实现。当前交付覆盖本地 durable memory 的工具写入、自动归纳、项目/全局隔离、请求前注入、检索、`memory_write` 审计事件和基础生命周期策略。
- **场景**:agent 在多次会话中累积"build 命令是 X / 测试方式是 Y / 仓库代码风格 Z"等
- **当前存储**:
  - 自动记忆:`$XDG_STATE_HOME/ub/memory/<project-key>/memory.md`(项目隔离、不进 git)
  - 全局指令:`$XDG_CONFIG_HOME/ub/instructions.md`(通常是 `~/.config/ub/instructions.md`,append-only,保护手写内容)
  - 工作区指令:`<workspace>/AGENTS.md` 由 S3-10 prompt harness 单独发现和注入,不走 `memory.max_chars`
- **当前写入与检索**:
  - 显式写入:`remember(text, category?, scope?)` 工具;`scope=auto` 写项目自动记忆,`scope=global` 追加全局指令,兼容旧的 `scope=workspace` 别名
  - 自动归纳:agent 成功完成 work/auto/full-access turn 后,前台只做低成本观察;后台调度器按显式记忆信号、累计 turn 数、累计可见消息数和最小间隔批量调用 `small_model`(未配置时复用当前模型),每次最多提取 `memory.auto.max_candidates` 条长期事实;plan 模式、显式 `remember` 已处理的 turn、以及默认配置下使用外部上下文的 turn 不自动写 memory
  - 检索:`recall(query?, category?)` 工具按关键词和分类搜索项目自动记忆
  - rollout/TUI 可见性:每次成功写入都会落 `memory_write` rollout 事件;`remember` 仍保留普通 tool activity + `tool_result`
- **当前读取**:每次发请求前把全局指令 + 项目自动记忆包进 `<memory>...</memory>` system message;`memory.max_chars` 控制注入预算,自动记忆按 category 优先级和时间截断
- **当前合并/衰减/隐私策略**:项目自动记忆按 category 做相似内容和同主题冲突合并并刷新时间戳;`debug`/`general` 低优先级条目会按时间衰减;超过容量时按优先级和新旧裁剪;写入前拒绝明显 credential/token/private-key 和临时 debug/stack trace 内容;全局指令保持 append-only
- **后续切片**:更细的用户可见 memory 管理 UI、手动删除/编辑入口、以及可配置的高级衰减策略
- **学习价值**:长期记忆 / RAG / 记忆生命周期管理
- **工程量**: L

### S3-03 Subagents(多 agent 协作)

**状态**:MVP 已实现,后续 UI/配置增强待定。

- **场景**:主 agent 派发独立 context 的子 agent 做调研 / 子任务,结果汇总回主流程
- **已落地**:`task("explore module X and report")` 工具按调用创建一次性 child runner,共享 provider/tools/mode,使用独立 context,跑完把最终消息作为父 tool result 返回;child 不创建独立 session,但 start/done/tool/permission activity 会镜像到父 session/turn,并用 `subagent:` 前缀与命名空间 tool id 提供可恢复 trace
- **当前架构约束**:agent 执行器保持轻量无状态,session/history/rollout 由 runner/store 外置管理,通过 provider cache 复用 provider/client,通过 `agent.Factory` 复用构造模板,不通过 agent 对象池承载对话状态
- **完整版**:子 agent 有独立 model / tools / mode 配置
- **UI**:TUI 中并列显示多个 agent 状态(类似 tmux 多 pane)
- **学习价值**:多 agent 编排、context isolation、token 经济、协作模式
- **工程量**: L
- **依赖**: §4.1 agent loop 解耦先做

### S3-04 Plan/Todo 分离的 Plan-then-execute

**状态**:已实现。`plan_*` 管 durable plan artifact,`todo_*` 管短生命周期执行清单,TUI/rollout/resume 均能重建执行视图。

把 `plan` 模式从"工具拦截"升级到"plan artifact + execution todo"工作流。

- **plan 阶段**:模型输出 `$XDG_STATE_HOME/ub/plans/<project-key>/<id>.md`,列出步骤、风险、回滚策略
- **review**:user 在 TUI 内通过完成摘要里的 `plan_id`、`/plans` picker 或 `/plan-edit <plan-id>` 打开 plan markdown,可编辑
- **execute 阶段**:切到 `work` 模式,agent 按 plan 一步步执行,每步对照 plan 检查
- **工具边界**:`plan_*` 与 `todo_*` 做成两套工具;`plan_write` / `plan_update` 负责 plan 模式下的持久化计划创建与修订,`plan_update_step` 负责 work/auto 执行阶段进度,`todo_write` / `todo_update` 负责当前 turn/session 的待办清单和即时执行状态
- **todo 语义**:todo 是短生命周期执行视图,不直接复用 plan markdown 的 checkbox;状态最小集为 `pending` / `in_progress` / `completed` / `skipped` / `failed`,并约束同一清单最多一个 `in_progress`
- **TUI 渲染**:TUI 增加 Claude Code / Codex 风格的独立待办列表渲染,从 `todo_*` tool result 或 agent event 更新;`todo_*` tool block 只保留审计摘要,待办列表支持运行中常驻展示、后续更新原地刷新、resume 后从 rollout/session state 恢复
- **可中断**:任意步骤中断后下次能从断点继续
- **验收标准**:
  - plan 工具仍只管理 state-root 下的 plan artifact,不承担实时 UI 状态
  - todo 工具可独立维护当前执行清单,普通多步骤任务无需先创建持久 plan
  - TUI 中能看到当前待执行、执行中、已完成/失败的任务列表,工具调用更新后即时刷新
  - rollout 记录 todo 创建/更新事件,便于 resume、调试和后续 eval 统计
- **学习价值**:长任务编排、可恢复 agent、self-check
- **工程量**: M-L

### S3-05 multiedit 工具

- **状态**:已实现。`multiedit` 已作为写类工具接入 preview / permission / TOCTOU / file-history 路径,支持一次调用跨文件多处编辑。
- **场景**:同一个 tool call 做多个文件的多处编辑,减少 round trip
- **schema**:`{edits: [{path, old, new, replace_all?}]}`
- **实现要点**:原子性(任一失败回滚)、preview 合并 diff 列表
- **工程量**: S

### S3-06 Tool I/O streaming

- **状态**:已实现 TUI-visible streaming。`bash` 和 `job_output --follow` 通过 `StreamingTool` 推送 stdout/stderr partial output,TUI 可在工具运行中显示滚动预览;provider tool-call 协议仍只在最终 `tool_result` 时把结果回灌给模型。
- **目标**:tool 通过 channel 流式回传 partial result,TUI 实时显示;模型驱动的运行中 interrupt 仍需要新的增量 tool-result 协议,不属于当前实现边界
- **参考**:OpenCode `packages/opencode/src/tool/shell.ts` 的 `Stream.runForEach` 模式
  - chunk 写入 buffer 的同时 `ctx.metadata({ output })` 推到 UI,UI 拿"最新 N 字节"做滚动预览
  - 用 `Effect.raceAll([exitCode, abort, timeout])` 三路竞速,任一触发都干净退出 + `forceKillAfter: 3s`
- **当前实现**:
  - `tool.Tool` 保持兼容,新增可选 `StreamingTool` interface;非 streaming tool 继续走同步 `Execute`
  - `internal/app/ub/agent/agent_tool_runner.go` 在 streaming path 中起 goroutine 执行工具,并把 `StreamEvent` 转发为 `EventToolPartialOutput`
  - `internal/app/ub/tui/` 支持 running partial output 与最终 done/failed 详情合并渲染
  - 第一批接入:`bash`、`job_output --follow`;未来可接入 `lsp.diagnostics --watch`
- **实现边界**:当前 partial output 已进入 EventSink/TUI,用户可通过取消请求触发 ctx cancellation;provider tool-call 协议仍按单个最终 `tool_result` 回灌,模型不会在同一 tool call 运行中收到 partial 并主动中断。若后续要做 model-driven interrupt,需要新增 provider/tool runner 的增量 tool-result 协议设计。
- **学习价值**:流式 IO、channel 设计、cancellation 语义、partial state 渲染
- **工程量**: L
- **风险**:partial output 与最终 result 的合并、截断、取消语义要保持一致,避免 TUI 展示运行中内容后被空详情覆盖

### S3-07 LSP 工具扩充

- **状态**:已实现。LSP 工具当前包括 `diagnostics`、`references`、`hover`、`completion`、`document_symbols`、`rename`、`code_action`。
- `rename`、`code_action` 只返回语言服务器建议,不直接写盘;模型需要用 `multiedit` / `edit` 套 ub 的 preview/permission 协议落地
- **价值**:agent 能做更细粒度代码操作
- **工程量**: M

### S3-08 Tool-result snapshot 工具

- **状态**:已实现。agent 经常想看上一次 tool 的输出做 follow-up,但 tool result 可能已被压缩成 inline preview。
- 提供 `tool_result(tool_use_id, offset?, limit?)` 工具:从当前 session 的 spillover 目录读回某次 tool call 的完整输出。
  - `tool_use_id` 来自 tool activity / rollout / truncation footer;agent 调用前通过 context 注入当前 `session_id`
  - 实际读取 `<state-root>/tool_outputs/<sessionID>/<toolUseID>.txt`,或 `context.tool_results.spillover_dir` 指向的替代根目录
  - 支持 1-based `offset` 和 `limit`,返回带行号的文本,并复用 read 工具的最大行数保护
  - 不接受任意路径,不跨 session 读取,也不再按 `turn_id` 从 rollout 反查
- **学习价值**:event log 的反向消费
- **工程量**: S

### S3-09 长输出自动落盘 + spillover 协议

- **状态**:已实现。tool result 在进入下一次 provider 请求和写入 rollout 前统一经过 `tooloutput.LimitResult` 限幅;超限内容自动落盘。
- **当前协议**:
  - 小输出保持 inline,默认上限由 `context.tool_results.inline_max_bytes`(12KiB) 和 `context.tool_results.inline_max_lines`(400) 控制
  - 大输出或带 `FullContent` 的结果写入 `<state-root>/tool_outputs/<sessionID>/<toolUseID>.txt`;配置了 `spillover_dir` 时写入该替代根目录
  - 落盘内容受 `context.tool_results.full_max_bytes`(默认 50MiB) 保护,截断时保持 UTF-8 边界并在文件尾写入 spillover 截断说明
  - 模型可见内容是预算内的前缀 preview + footer:`... [tool result truncated: original_bytes=N]`、`full_output_path=<path>` 和 read 工具继续查看提示
  - `tool.Result` / rollout payload 同步记录 `truncated`、`original_bytes`、`full_output_path`,TUI 展开详情会保留这些 footer
  - `bash` 结果仍保留 `<shell_metadata>`(exit_code / duration / timeout / aborted 等);`job_output` 返回 job_id/state/exit_code/stdout_total/stderr_total 和 stdout/stderr 快照
  - 后续 turn 可用 `read` 读取 `full_output_path`,或用 S3-08 的 `tool_result(tool_use_id, offset?, limit?)` 读取同 session spillover
- **配置**:`context.tool_results.inline_max_bytes` / `inline_max_lines` / `spillover_enabled` / `spillover_dir` / `full_max_bytes` / `spillover_max_age`
- **学习价值**:tool result 的"压缩 + 索引"语义;rollout / state / cwd 三种存储边界的取舍
- **工程量**: M
- **实现边界**:当前 spillover 在最终 tool result 阶段落盘;S3-06 streaming partial output 负责 TUI 运行中预览,不等同于边流式边写完整输出
- **测试**:小输出走 inline、大输出走落盘 + 路径提示、UTF-8 多字节边界不切坏、`tool_result` 支持 offset / limit

### S3-10 Prompt harness + workspace instructions

**状态**:S3-10A 已实现;S3-10B/S3-10C 与 S4-06/S4-07 合并进入后续 prompt/context 可靠性主线。当前已有 coding-agent 指令、workspace `AGENTS.md` 注入、git snapshot、structured compact prompt 和行为回归测试,但还缺统一 section registry、prompt inspect 和 provider/cache 策略。

把"同一个模型在 ub 里不够聪明"拆成可观察、可测试的 harness 问题,而不是只归因于模型能力。

- **问题定义**:
  - 同一模型在不同 CLI 中表现差异,通常来自上下文、工具 affordance、默认行为规则和压缩策略的差异
  - `ub` 目前已经有 runtime context、memory、plan artifact 和 tool result spillover,但缺少统一的 coding-agent 行为层
  - 成功标准不是"prompt 更长",而是模型更稳定地读对文件、选对工具、遵守仓库规则、诚实汇报验证结果
- **目标**:补齐 coding agent 的系统提示词、动态上下文和工具级指导,提升模型在代码任务中的默认行为稳定性
- **参考**:Claude Code 的分层 system prompt、tool-specific prompt、workspace instruction 自动发现、git snapshot、structured compact prompt
- **MVP 范围(S3-10A)**:
  - 主系统提示词分层:任务执行原则、风险动作确认、优先使用专用工具、简洁沟通、失败后先诊断再换策略
  - workspace instructions:启动时自动发现并注入工作区根目录的 `AGENTS.md`,支持 max chars 和显式关闭
  - git snapshot:会话开始时注入当前 branch、默认 branch、`git status --short`、最近提交;明确标注为启动时快照,不伪装成实时状态
  - 工具级 prompt:强化 `bash` / `read` / `grep` / `task` / `plan_write` / `plan_update` / `plan_update_step` 的 description,把"什么时候用/不用"写到工具说明里
  - structured compact prompt:替换当前短 summary 模板,要求保留用户意图、关键文件、错误与修复、用户反馈、当前工作和下一步
- **后续切片(S3-10B/S3-10C)**:
  - 增加 `ub prompt inspect` 或 `ub doctor --prompt` 调试入口,展示本次请求启用了哪些 prompt section、截断了哪些上下文
  - 基于 rollout 增加小型行为评测:read-vs-ls、先读后改、测试失败不报成功、复杂任务更新 plan
  - 支持 provider/model 级 prompt profile,例如小模型使用更强工具指导,大模型使用更短提示
- **配置**:
  - `prompt.workspace_instructions.enabled`
  - `prompt.workspace_instructions.max_chars`
  - `prompt.git_snapshot.enabled`
  - `prompt.git_snapshot.max_chars`
  - `prompt.compact_style`(short / structured)
- **实现要点**:
  - 不照搬长 prompt;保持 ub 风格:短、可读、可测试、可配置
  - prompt 内容分静态 section 和动态 section,为未来 prompt cache / provider 差异化预留边界
  - workspace instruction 与 memory 分开:前者是用户/仓库显式规则,后者是长期经验沉淀
  - instruction discovery 默认不越过 workspace root;若未来 workspace 归一化到 git root,再考虑按目录层级合并
  - git snapshot 只读、超时短、失败静默降级,避免启动变慢
- **非目标**:
  - 不实现完整 prompt DSL 或任意用户覆盖系统提示词
  - 不把 Claude Code 的提示词文本复制进仓库;只借鉴结构和测试方式
  - 不把 S3-02 的长期记忆写入策略混进本条;本条只负责读取和注入显式上下文
  - 不引入 provider 端持久 conversation state;这属于 S4-04
- **与其他条目的边界**:
  - S3-02 负责长期记忆的写入、合并、衰减;S3-10 负责把显式规则和当前 workspace 状态注入请求
  - S3-03 负责子 agent 编排;S3-10 只强化 `task` 工具的使用说明和子任务 prompt 写法
  - S3-04 负责 plan artifact 生命周期;S3-10 只强化模型何时写 plan、何时更新 step、何时不能标完成
  - S4-06 负责 prompt builder 的代码结构;S3-10 先定义用户可感知能力和验收行为
- **验收标准**:
  - prompt builder unit test:不同配置下 section 顺序、截断、关闭行为稳定
  - instruction discovery test:目录层级、重复文件、超长文件、缺失文件
  - git snapshot test:非 git repo、dirty repo、超长 status、命令失败
  - fake provider 行为回归:目录用 `ls`/`glob` 而不是 `read`;复杂任务会先读文件再改;失败测试不会被报告为通过
  - 手测:`ub run --dev` 能看到模型在复杂任务中先定位文件、再编辑、再验证,最终汇报包含真实验证状态
- **学习价值**:prompt engineering 从"写一段神秘系统提示词"升级为可演进的 agent harness
- **工程量**: M
- **依赖**:可先独立做最小版;完整形态与 S4-06 prompt builder 分层配合

### S3-11 Structured ask tool

**状态**:已实现。新增 `ask` 结构化偏好询问工具,在 work/auto/plan 模式可用且不走 permission approval;TUI 会渲染单选/多选 chooser 并把用户选择摘要写回 transcript。headless `ub run` 没有交互 asker 时不阻塞,而是返回让模型自行判断并说明假设的 tool result;子 agent 默认不继承 asker。

让模型在真正需要用户做选择时,通过结构化工具询问用户,而不是用普通 assistant 文本把任务停住。

- **场景**:
  - 需求存在实质分叉:技术路线、范围取舍、第三方库选择、是否接受破坏性迁移
  - 代码和上下文无法可靠推断用户偏好,但继续猜测会导致明显返工
  - plan 模式中需要用户确认方向,但还不涉及工具权限审批
- **非目标**:
  - 不替代 permission modal;工具执行审批仍由 `internal/pkg/runtime/permission` 管
  - 不鼓励模型遇到小问题就问;有合理默认值时应直接选择并说明假设
  - headless `ub run` 不应无限阻塞;没有交互前端时返回"自行判断并说明假设"的 tool result
- **接口草案**:
  - 工具名:`ask`
  - 风险:`RiskSafe`,plan 模式可用,不需要 permission approval
  - schema:`{questions:[{header, question, options:[{label, description}], multi_select?}]}`
  - TUI 渲染为 pinned chooser/modal,用户选择后把答案格式化为 tool result 回给模型
- **实现要点**:
  - 新增 agent-level `Asker` 接口,与 `permission.Asker` / `LimitAsker` 分开,避免把"问用户偏好"混进工具审批
  - agent 执行 tool 时把 asker 注入 tool context;子 agent 默认不继承 asker,除非未来 UI 能清楚呈现子 agent 提问来源
  - rollout 持久化 ask request / answer 事件,便于 resume 和审计
  - TUI 要支持单选、多选、取消/跳过,并在 transcript 中保留用户选择摘要
- **参考**:`.references/DeepSeek-Reasonix/internal/agent/ask.go` 的 read-only `ask` tool 和 CLI chooser
- **学习价值**:把"澄清问题"作为 agent-tool 协议建模,区分用户偏好、权限审批和普通对话
- **工程量**: M
- **依赖**:可独立实现;若先做 §4.2 事件总线,ask request/answer 事件可以直接接入统一事件流

### S3-12 WebSearch / WebFetch 工具

ub 已有内置 `web_search` / `web_fetch` 联网工具,不再只能通过 MCP 搜索 server 或 `bash` + `curl` 间接完成。V2 仍把联网检索作为 first-class tool 持续演进,重点是来源归因、权限、限幅和审计。

**状态**: 已实现。`tools.web.enabled` 默认开启内置 `web_search` / `web_fetch`;搜索默认使用无需 API key 的 DuckDuckGo provider,也支持 Brave/Tavily/SerpAPI/SearXNG provider;抓取支持 HTTP(S) HTML/PDF/text 的最小解析、robots/timeout/redirect/大小限制、本地/私网默认阻断。联网工具使用独立 `RiskNetwork`,plan 模式不广告且会被 mode gate 拒绝,work/auto/full-access 走 permission/rule 审批;rollout `tool_result.metadata` 和 `rollout show` 可审计 query、URL、provider、parser、截断/落盘信息。

- **场景**:
  - 用户明确要求"查最新"、"搜索一下"、"看某个网页/issue/文档"
  - 代码任务依赖当前外部事实:API 文档、release notes、CVE/安全公告、包版本、错误信息搜索
  - 本地仓库和已有上下文不足以回答,继续猜测会有高概率过时或错误
- **工具边界**:
  - `web_search(query, recency?, domains?, limit?)`:返回搜索结果标题、URL、摘要、时间;不直接抓取全文
  - `web_fetch(url, max_chars?)`:抓取指定页面/文档的正文摘要与少量引用片段;支持 HTML/PDF 的最小可用解析
  - 搜索结果和抓取正文都按 `context.tool_results` 统一限幅,完整抓取内容可走 tool-output spillover
  - `rollout show` 必须能看到 query、URL、来源和截断/落盘 metadata,便于审计
- **权限与配置**:
  - 默认不在 plan 模式广告;work/auto 模式默认启用,也可通过 `tools.web.enabled: false` 关闭
  - 网络请求属于独立 `RiskNetwork` 或复用 `RiskExec` 审批路径,需要在 permission UI 中明确展示目标域名和数据外发风险
  - 支持 allow/deny domain 规则,例如 `WebFetch(docs.python.org:*)` / `WebSearch(domain:golang.org)`
  - 配置搜索 provider:默认 DuckDuckGo 不需要 key;也支持可插拔 backend(如 Tavily/Brave/SerpAPI/自建 SearXNG),商业 provider 缺 key 或 SearXNG 缺 base_url 时给出清晰 tool error
- **实现要点**:
  - tool 层只暴露 provider-neutral 结果,避免把某家搜索 API 的响应格式泄漏给模型
  - 页面抓取要做 robots/timeout/redirect/内容类型/大小限制,禁止访问本地网段和 file URL
  - 引用输出要短而可追溯:每条结论带 URL,长文只摘要不大段搬运
  - MCP 搜索 server 仍然可作为替代/扩展路径,但内置 WebSearch 负责统一权限、限幅、rollout 和 TUI 呈现
- **非目标**:
  - 不做完整浏览器自动化、登录态复用、表单提交或任意 JS 执行
  - 不把搜索 API key 写入 rollout、日志或模型上下文
- **学习价值**:联网工具的权限模型、来源归因、外部内容限幅与审计、provider-neutral tool abstraction
- **工程量**: M-L
- **依赖**:建议在 S3-10 prompt harness 后做,并与 §4.2 事件总线 / §4.3 tracing 联动;最小 MVP 可先独立实现 `web_search` + `web_fetch`

### S3-13 Model-initiated plan mode

**状态**:已实现。新增 `enter_plan_mode` / `exit_plan_mode` 安全工具和 TUI 确认流;work 模式可由模型请求进入 plan,plan 模式可带 `plan_id` 请求批准退出并恢复本进程 `pre_plan_mode`;auto/full-access 默认不广告入口。mode 切换写入独立 `mode` activity 和 rollout 审计,缺少 `plan_id` 的退出请求会直接返回明确 tool error。

让模型在判断任务需要先调研和对齐方案时,可以主动请求进入 plan 模式,而不是只能依赖用户提前通过 CLI / config / `/mode plan` 切换。

- **问题定义**:
  - ub 当前 `execution_mode` 是启动参数、配置、profile 或 slash command 决定的运行时策略;模型无法在 work 模式中表达"这个任务值得先计划"
  - 对复杂实现任务,模型如果直接开改会增加返工;如果只用普通文本问"要不要先计划",又缺少结构化状态切换、TUI 呈现和 rollout 记录
  - 之前已明确 mode 不随 session 持久化;本功能不能重新引入 resume 恢复退出前 mode 的设计
- **参考行为**:
  - Claude Code 把 `EnterPlanMode` 做成模型可调用 tool,tool prompt 告诉模型在非简单实现任务前主动使用
  - 进入 plan 时记录 `prePlanMode`,退出 plan 时恢复原 mode;`/plan` 命令和 tool 入口共用同一条状态转换路径
  - plan mode 的系统提示/attachment 约束模型只做读代码、写 plan artifact、必要时 ask,最后用 `ExitPlanMode` 请求用户批准计划
- **MVP 范围**:
  - 新增 `enter_plan_mode` agent tool:无输入,`RiskSafe`,默认只在 `work` 模式广告;`plan` 模式隐藏;`auto` 模式默认不广告,避免打断用户选择的连续执行语义
  - 模型调用后弹专用 TUI dialog:`Enter plan mode?`,说明将只读调研并产出 plan;用户允许则当前进程 mode 切到 `plan`,拒绝则 tool result 告诉模型继续在当前 mode 工作
  - runtime state 增加内存态 `pre_plan_mode`,只用于本进程内 `exit_plan_mode` 恢复;不写 session 元数据、不写配置、不影响 resume 的有效 mode 规则
  - 新增 `exit_plan_mode` agent tool:仅 plan 模式可用,展示当前 plan artifact 给用户批准;批准后恢复 `pre_plan_mode`(缺失则回到本次启动有效 mode 或 `work`),拒绝后留在 plan 模式并提示模型修订 plan
  - `enter_plan_mode` 不创建 plan 文件;进入后的 prompt / tool description 引导模型使用现有 `plan_write` / `plan_update`,并在用户纠正方案时原地更新同一个 plan
- **Prompt 规则**:
  - 在 S3-10 prompt harness 中增加 tool-specific prompt:新功能、多方案取舍、架构调整、多文件行为变更、需求模糊时优先请求 plan
  - 简单 typo、小范围明确 bugfix、用户已经给出细节实现路径、纯只读调研时不要进入 plan
  - auto 模式 prompt 明确"优先行动,除非用户明确要求,不要主动进入 plan";如果未来允许 auto 中显式进入,必须先过用户确认
  - 与 S3-11 `ask` 的边界:需要用户在几个具体选项中选择时用 `ask`;需要先调研并产出整体方案时用 `enter_plan_mode`
- **状态与事件**:
  - mode 切换产生 `Activity` / rollout event:记录 `source=tool|slash|startup`,from/to mode、是否 user-approved、关联 tool_use_id
  - TUI message list 用独立 mode activity block 展示 `Enter Plan Mode` / `Exit Plan Mode`,不混进 command permission activity
  - resume 只重放历史 activity 供审计,不把历史 mode 作为当前 session 的有效 mode
- **非目标**:
  - 不做 session 级 mode 持久化
  - 不让模型绕过 plan 模式的 tool gate;进入 plan 后仍只广告 read/search/plan/ask 这类安全工具
  - 不把 Claude Code 的长 prompt 原文复制进仓库;只复用"模型 tool 请求 + 用户批准 + pre_plan_mode 恢复"这个产品结构
- **验收标准**:
  - work 模式下,复杂任务的 fake provider 能调用 `enter_plan_mode`,TUI 批准后只暴露 plan 工具,`exit_plan_mode` 批准后恢复 work
  - auto 模式下,默认 prompt / tool advertisement 不诱导模型主动进入 plan
  - 拒绝进入 plan、拒绝退出 plan、缺少 plan artifact 调用退出工具都有明确 tool result
  - `rollout show` 能看到 enter/exit plan 的 tool input/result 和 mode activity;resume 后能看到历史 activity,但当前 mode 仍按本次启动规则计算
- **学习价值**:把"模型觉得需要规划"建模成可审计的 tool 协议,练习 agent-driven mode transition、plan artifact 生命周期和 prompt affordance
- **工程量**: M
- **依赖**:建议在 S3-04 plan artifact 和 S3-10 prompt harness 后做;可与 S3-11 structured ask tool 并行,共享 TUI modal/answer 回灌能力

### S3-14 Full-access / bypass-permissions mode

**状态**:已实现。补齐一个明确的 full-access 模式,用于用户已经确认愿意让 ub 在当前 workspace 内连续执行命令和文件变更、不中途弹权限框的场景。它是独立 execution mode,不是 `auto` 的别名: `auto` 仍然是 approval agent 自动判断 + human fallback,full-access 则是显式跳过常规审批。

- **问题定义**:
  - ub 当前只有 `work` / `plan` / `auto`;`work` 遇到 exec 风险默认问人,`auto` 依赖 approval agent,没有"我已经信任本次会话,直接执行"的模式
  - 用户需要批量修复、跑长测试、反复执行项目内命令时,频繁 permission modal 会拖慢工作流
  - 如果把 full-access 混进 `auto`,会模糊"模型自行判断"和"用户显式授权跳过审批"两种完全不同的风险边界
- **目标语义**:
  - 新增 `execution.ModeFullAccess = "full-access"`;配置、CLI `--mode`、TUI `/mode` 和 Shift+Tab cycle 都能识别
  - full-access 下,`RiskWrite` 和 `RiskExec` 默认允许执行,不走 human asker,也不走 approval agent
  - 仍然保留硬安全边界:内置 denylist、project deny rules、不可解析/明显越权的 compound command、未来 `RiskNetwork` 的本地网段/file URL 禁止等,不被 full-access 绕过
  - plan 模式仍然优先:进入 plan 后保持 read-only tool gate,不能因为 `pre_plan_mode=full-access` 就在 plan 内执行写入/命令
- **UI / UX**:
  - 当前实现不提供第一次切到 full-access 的高风险确认 dialog;CLI/config/TUI slash/Shift+Tab 的显式切换即为本次进程内授权
  - 状态栏用与 `work/plan/auto` 区分明显的标签,例如 `full-access`,并在帮助里说明风险
  - Shift+Tab 已纳入 full-access cycle;需要更保守入口时可在后续单独增加首次确认或从快捷循环移除
  - 不提供 "always allow full-access globally" 之类持久授权;配置文件可以显式设置,但 TUI 运行中切换默认只影响当前进程
- **权限与审计**:
  - permission manager 增加 `SourceMode` 或 `SourceFullAccess` 的 allow 结果,rollout 中记录每次被 full-access 放行的 tool、risk、command 摘要和 cwd
  - `permissions.deny` 与硬 denylist 优先级高于 full-access;`permissions.ask` 在 full-access 中是否强制询问需要明确,建议仍强制 ask,让用户可以对高风险命令设置例外保护
  - `rollout show` 明确标出 `allowed by full-access mode`,避免事后看不出为什么没有弹权限框
- **实现面**:
  - 更新 `internal/pkg/core/execution.ParseMode` / `Gate`、`internal/pkg/core/config.ValidateExecutionMode`、schema、docs/requirements 和 docs/design 的 mode 枚举
  - 更新 `internal/pkg/runtime/permission.Manager.Ask`:在 deny / hard safety / ask 规则之后,full-access 直接 allow;auto 分支保持不变
  - 更新 TUI mode picker、status style、help 文案、pending permission modal 上的 mode 显示
  - 更新 fake provider / permission manager / TUI mode cycle 测试,覆盖 full-access 放行、deny 优先、ask 优先、plan gate 优先和 resume 不持久化运行中切换
- **非目标**:
  - 不绕过操作系统、文件系统 sandbox 或未来 sidecar 的外部隔离
  - 不允许模型隐藏执行;tool activity 和 rollout 仍必须完整记录
  - 不把 full-access 作为默认 mode,也不在 agent prompt 中鼓励模型自行请求进入
- **学习价值**:把高信任/低摩擦执行和 auto approval 明确拆开,练习危险模式的 UX、规则优先级和审计设计
- **工程量**: S-M
- **依赖**:可独立实现;建议在 permission activity 持久化和 `rollout show` tool input 完成后做,这样审计链路先可靠

### S3-15 Goal mode

**状态**:已实现。goal mode 是 v0.4.3 后加入的长任务连续执行入口,用于让 agent 围绕一个明确目标跨多轮推进,直到目标完成、阻塞或预算耗尽。

- **场景**:
  - 用户明确希望 ub 自主推进一项长任务,例如连续修复一组失败、完成一轮重构或跑完一串验证。
  - 主流程需要一个可审计的 objective,避免普通多轮对话在上下文压缩后丢失"终局条件"。
- **当前语义**:
  - `create_goal(objective, token_budget?)` 创建当前 session 的 active goal;已有 goal 时拒绝重复创建。
  - `get_goal()` 返回目标、状态、预算和用量;TUI 状态栏展示 goal 状态。
  - `update_goal(status="complete")` 只在 objective 真正完成时调用,并在有预算时报告最终 token 用量。
  - goal 状态不替代 plan/todo:plan 负责方案 artifact,todo 负责当前执行清单,goal 负责跨 turn 的目标约束和完成判定。
- **后续切片**:
  - goal 与 context compaction 更紧密集成,确保压缩摘要保留 objective、完成条件、阻塞历史和剩余预算。
  - 增加 goal-level eval,记录长任务成功率、阻塞原因和平均 turn/token。
  - 与 future `ub prompt inspect` / tracing 联动,让用户能解释"为什么还在继续"或"为什么停止"。
- **学习价值**:长期自主任务的目标状态、预算约束和可审计停止条件。

### S3-16 Session fork / branch / handoff

**状态**:候选,建议在 artifact 引用层稳定后做 MVP。灵感来自 `.references/oh-my-pi` 的 session tree / fork / handoff,但 ub 不以复刻完整树形 UI 或云端分享为目标。

- **场景**:
  - 从历史 turn 派生新 session,保留当前 session 不变,避免 `/rewind` 只能修改线性时间线
  - 从同一 checkpoint 使用不同 provider/model/策略继续,为跨 provider eval 提供可比样本
  - 长会话需要切到干净上下文时,生成 handoff 文档并建立父子 session 关系
- **MVP**:
  - `ub sessions fork <session-id> --from-turn N` 与 TUI `/fork [turn]`
  - 新 session 记录 `parent_session_id`、`fork_turn`、`fork_event_id` 和初始 provider/model
  - 复制或引用目标 turn 之前的 rollout/context,不修改父 session;文件 checkpoint 默认只用于提示和审计,不隐式切换 workspace 文件状态
  - artifact、plan、tool output 等引用必须通过 S4-09 的 session-aware 引用规则处理,不能复制失效的绝对路径
- **后续候选**:session tree picker、branch summary、从当前会话生成 handoff 后启动新 session。在线 share / relay 不属于本条 MVP。
- **验收标准**:
  - fork 后父 session 内容不变,子 session 可独立 resume/rewind
  - 同一 checkpoint 派生多个 session 时,rollout 能恢复父子关系并支持 eval 分组
  - session 删除和清理不会误删仍被父/子 session 引用的 artifact
- **学习价值**:事件溯源分支、可复现实验、会话可移植性
- **工程量**:M

---

## §4 架构优化

**目标**:每条都让 ub 比当前更易读、更易 fork、更适合做研究脚手架。

### S4-01 Agent loop 解耦

**状态**:已完成第一轮包内解耦。原先的单体 agent loop 已拆到 `agent_stream.go`、`agent_tool_runner.go`、`agent_limits.go`、`tool_modes.go`、`activity_*`、`memory_auto.go`、`event_record.go` 等文件;`Agent` 仍保留在 `internal/app/ub/agent` 包内,通过 `Options` / `Factory` 注入 provider、tool registry、permission、rollout、context、prompt、hook、memory、file history 等外置状态。

原计划的五个子包仍是后续可选方向,不作为当前完成标准:
- `agent/prompt`:纯函数,接收 history + tools + ctx + memory,产出 messages
- `agent/dispatch`:从 provider event 提取 tool calls
- `agent/exec`:并发执行 tool(集成 permission)
- `agent/ctx`:估算 token,决定是否触发 compaction
- `agent/loop`:协调以上四块的 orchestrator

**价值**:贡献者可以单独替换其中一块做实验(reflexion loop / tree-of-thought / lookahead planner...)。

**工程量**:已完成 M 级包内拆分;若继续拆子包仍是 M-L
**测试**:当前依赖 agent/fake provider/工具模式/streaming/summary 等单测覆盖;若拆子包,每个子包需要独立 unit test

### S4-02 事件总线

**状态**:未实现,但仍是中期架构主线之一。当前 agent/TUI/rollout/spillover/log 已经能工作,因此 eventbus 应该服务于可观测性和可插拔订阅,不应为了"更干净"做大拆迁。

现状:rollout / TUI / spillover / log / 未来的 webhook 各自订阅 agent 事件,代码散落。

抽象 `internal/eventbus`:
- 一处发布,多处订阅(channel fan-out)
- 类型化 topic(`ToolCallStart` / `ToolCallEnd` / `Usage`...)
- 可挂载外部 webhook subscriber

**价值**:事件驱动架构 / pub-sub 教学样本

**工程量**: L

### S4-03 OpenTelemetry tracing

**状态**:未实现。建议在 event taxonomy 稳定后启动,也可以先做最小 local trace/span recorder,再接 OTel exporter。

- 每个 turn / tool call / provider request 一个 span
- 本地用 Jaeger / honeycomb 看 agent 决策路径
- 副产物:"为什么这个 turn 花了 30 秒"分析
- **工程量**: M
- **学习价值**:分布式追踪 / 性能分析 / agent 调试

### S4-04 Provider 抽象升级

**状态**:未实现。当前 provider 仍以 stateless request + rollout replay 为主,这是 ub 可调试性的核心边界;provider state/Responses API 需要先回答是否破坏 replay 语义。

- **OpenAI Responses API**:多步 conversation,state in provider(避免重复发完整 history)
- **Anthropic memory tool**:让 provider 端管理 memory
- **结构化输出统一**:function calling / JSON mode / Anthropic tool 抽象成一个 interface
- **Thinking 内容统一**:OpenAI reasoning / Anthropic extended thinking / DeepSeek `reasoning_content` 抽象

**工程量**: M-L
**学习价值**:provider 适配层是观察 LLM 生态演化的好窗口

### S4-05 Sidecar tool process

**状态**:推迟到 v1.0 之后再评估。

把内置 tool 也走 MCP 协议(子进程 + stdio JSON-RPC),内置 / 外部 tool 用同一套接口。

- **价值**:协议统一、进程隔离、安全(tool crash 不影响主进程)
- **风险**:fork 进程开销、IPC 延迟、不稳定 tool 调试更难
- **工程量**: XL
- **推荐**:**先不做**,等 §4.2 事件总线和 §3.6 streaming 沉淀经验后再说

### S4-06 Prompt builder 分层与测试快照

**状态**:第一垂直切片已实现。当前已有固定顺序的 prompt section registry，main/read-only/no-tool 构造共用同一模型，并提供默认脱敏的 `ub prompt inspect` 文本/JSON manifest；summary/tool guidance 的进一步分层与 provider CachePlan 仍待后续切片。

把 prompt 构造从 agent loop 中独立出来,让 system prompt、runtime context、memory、workspace instructions、tool guidance、summary/compact prompt 都能独立演进和测试。

- **现状**:`internal/app/ub/agent` 已为 coding-agent、runtime、workspace instructions、git snapshot、execution mode、memory 建立稳定 section ID、顺序、状态、来源、字符/token 估算和截断元数据;summary 模板和 tool schema 尚未纳入统一 inspect/cache plan
- **目标结构**:
  - `agent/prompt/system`:静态 coding-agent 指令、风险动作、工具使用原则、沟通风格
  - `agent/prompt/runtime`:cwd、shell、OS、路径规则、execution mode
  - `agent/prompt/workspace`:workspace instructions、git snapshot、memory
  - `agent/prompt/tools`:工具 description 组装、动态 MCP 工具说明、工具使用偏好
  - `agent/prompt/compact`:summary / compact prompt 模板和变体
- **设计要点**:
  - section 有稳定 ID、顺序和开关,方便 snapshot test 与后续 prompt cache
  - 动态 section 与静态 section 分离,避免 git status / MCP 变化导致整段 prompt 无法复用
  - provider 差异只在 section 选择和 tool schema adapter 中处理,不把 Anthropic/OpenAI 细节散进 agent loop
  - summary prompt 明确 no-tools 语义,即使未来 summary 复用完整工具集也不应触发工具调用
- **验证**:
  - registry/manifest 测试覆盖默认配置、plan mode、no-tool、无 git repo、无 memory、disabled section 与显式正文
  - fake provider 覆盖 prompt 变更后的关键行为,避免只测字符串不测 agent 行为
  - 性能测试记录 prompt 构造耗时和注入字符数,防止 workspace instructions 失控
- **学习价值**:把 prompt 当作可维护代码,而不是散落在运行时里的字符串
- **工程量**: M
- **依赖**:建议与 S4-01 agent loop 解耦一起做,也可先抽出最小 `agent/prompt` 包

### S4-07 Provider prefix cache optimization

**状态**:未实现,是 S4-06 之后的高价值切片。当前 usage 字段已经能承载 cache read/write 统计,但请求构造还没有主动 cache breakpoint 或稳定 cache plan。

把 prompt cache 从"usage 统计字段"推进到可验证、可调优的请求构造策略,优先覆盖 Anthropic 与 DeepSeek/OpenAI-compatible。

- **现状**:
  - `provider.Caps.SupportsPromptCache` 和 `Usage.CacheReadTokens/CacheWriteTokens` 已存在,但主要用于能力/统计
  - OpenAI-compatible 只归一化 `cached_tokens`;Anthropic 只读取 cache usage,未主动设置 cache breakpoint
  - roadmap 已在 S3-10/S4-06 要求 prompt section 稳定,但还没有 provider 侧 cache 策略
- **目标**:
  - 稳定复用 system prompt、workspace instructions、tool schema 等长前缀,减少重复输入 token
  - 把动态 section(git status、MCP 状态、最近 tool output)与静态 section 分开,避免小变化破坏整个前缀命中
  - 用真实 provider usage 验证 cache read/write,而不是只依赖 mock
- **Provider 策略**:
  - Anthropic:在 system / tools / conversation tail 的合适 block 上设置 `cache_control`,并用单测锁定 placement
  - DeepSeek/OpenAI-compatible:保持 repeated prefix 稳定,归一化 DeepSeek top-level cache hit/miss 与 OpenAI `prompt_tokens_details.cached_tokens`
  - reasoning 内容:不要把 OpenAI-compatible/DeepSeek 的 `reasoning_content` 作为下一轮历史输入回传;只作为 activity/rollout 展示,避免放大 prompt 或破坏 cache
  - 对不支持 prompt cache 的 provider,策略必须退化为普通请求,不改变语义
- **实现要点**:
  - 依赖 S4-06 的 prompt section ID/ordering;先实现最小 `CachePlan` 也可以
  - `ub doctor --json` 或未来 `ub prompt inspect` 展示本次请求的 cacheable sections、动态 sections 和 provider cache support
  - rollout usage 展示 cache hit/write token,并在 eval/report 中可汇总命中率
  - 增加 env-gated live probe,参考 `.references/DeepSeek-Reasonix/internal/provider/openai/realcache_test.go`,验证 DeepSeek repeated prefix hit 与 reasoning round-trip 风险
- **验收标准**:
  - Anthropic request builder 单测覆盖 cache_control placement
  - OpenAI-compatible usage parser 覆盖 DeepSeek hit/miss 与 OpenAI cached_tokens 两种格式
  - prompt builder snapshot 证明静态 section 在连续 turn 中保持稳定顺序和内容
  - live probe 文档化:如何用 DeepSeek/Anthropic key 跑一次 cache hit 验证
- **学习价值**:把 token 经济和 provider 协议差异变成可观察实验,而不是只看总 token 数
- **工程量**: M
- **依赖**:建议在 S3-10/S4-06 之后做;OpenAI-compatible usage 归一化可先独立补强

### S4-08 Context decision engine + semantic pruning

**状态**:未实现,是 v0.5 上下文可靠性的核心切片。当前已有请求前 token 估算、阈值 compact、手动 compact 和 provider overflow 后 compact-and-retry,但触发原因、裁剪收益、保护规则与 retry 路径还没有统一建模。

**已具备前置基础**:ContextWindowResolver 已统一显式配置、provider/model 静态能力与按 endpoint/model 隔离的 usage/overflow 观察，向 context event 暴露窗口 source/confidence；S4-08 可直接在此基础上构造 snapshot/decision。

参考 `.references/oh-my-pi` 的 compaction/pruning 思路,把上下文维护从单次 summary 调用升级为可观察的决策过程,但不引入其 snapcompact 图片归档等实验性复杂度。

- **核心模型**:`ContextSnapshot -> ContextDecision`
  - snapshot 记录 estimated/measured tokens、模型窗口置信度、最近 compact 边界、goal/plan/todo 状态和可裁剪 tool results
  - decision 明确 `keep` / `prune` / `compact` / `promote-model` / `compact-and-retry`,并记录 `reason=threshold|overflow|incomplete|mid_turn|idle`
  - rollout 记录 tokens before/after、cut boundary、pruned/protected tool ids、summary model、耗时和是否 retry
- **语义裁剪**:
  - tool result 可标记 `normal` / `useless` / `superseded` / `protected`
  - 优先裁剪已经被后续 read/search 覆盖的结果、空搜索结果和无新输出的 job polling
  - goal、当前 plan/todo、最近验证结果、用户纠正、error 和 active turn 的 tool-call/result 配对默认受保护
  - 不在 tool result 中间或不完整 user turn 边界切 compact;裁剪不得破坏 provider tool-call pairing
- **验收标准**:
  - fake provider 覆盖 threshold、overflow、incomplete 和 mid-turn 四种路径
  - eval 能比较 compact 前后任务完成率、关键信息保留率和 token 节省
  - `ub prompt inspect` / trace 能解释为何裁剪或 compact,而非只显示最终 token 数
- **学习价值**:上下文工程、决策可解释性、cache-aware pruning、长任务可靠性
- **工程量**:M-L
- **依赖**:S4-06 prompt section registry;事件先写 rollout,后续通过 S4-02 eventbus fan-out

### S4-09 Session-scoped artifact reference layer

**状态**:候选。当前 tool-result spillover 已按 session/tool-use-id 落盘并可用 `tool_result` 回读,但 rollout/TUI 仍携带 `full_output_path`,plan、goal、subagent 输出和未来 debug bundle 也各自管理路径与清理语义。

- **目标**:用稳定逻辑 ID 分离“会话里如何引用”与“磁盘上存在哪里”,优先支持 `artifact:<kind>:<id>` 或等价结构化引用,不要求照搬 oh-my-pi 的 URL 名称。
- **第一阶段**:
  - tool spillover payload 增加 `artifact_id`,保留 `full_output_path` 作为兼容/本地调试字段
  - `tool_result` 同时接受 tool-use-id 与 artifact-id,解析和越权校验统一放到 workspace artifact store
  - 建立引用计数或反向索引,让 session fork/delete/cleanup 不会产生悬空引用或误删共享 artifact
- **后续范围**:subagent report、plan、handoff、debug bundle;二进制图片等 content-addressed blob 等真实需求出现后再评估。
- **验收标准**:
  - session resume、fork、workspace 移动后逻辑引用仍可解析
  - 模型和默认 TUI 不依赖或泄露用户主目录绝对路径
  - 清理测试覆盖单 session、父子 session 和共享引用
- **学习价值**:内容寻址、资源生命周期、session portability
- **工程量**:M

### S4-10 Extension host boundary

**状态**:后置候选。当前 shell hooks 适合自动化,MCP 适合外部工具,内置 slash/tool/TUI renderer 则是静态注册;在 S4-02 eventbus 稳定前不启动完整 extension 系统。

- **分阶段边界**:
  1. typed event subscriber:只读订阅 turn/provider/tool/context/session 事件
  2. command/tool registration:注册 slash command 和受 permission 管理的工具
  3. lifecycle interception:在明确顺序、超时、错误隔离和可审计语义后,才允许修改/阻断请求
  4. renderer/provider registration:只有稳定外部需求时再开放
- **实现约束**:
  - 不使用 Go 原生 `plugin` 作为跨平台公共接口;优先考虑进程外 JSON-RPC/MCP 或受控 WASM
  - shell hooks 保持兼容,不强迫简单自动化迁移到 extension
  - extension 的工具调用仍必须经过 mode、permission、rollout、spillover 与 trace
- **启动条件**:至少两个真实的第三方扩展需求无法由 hooks/MCP 满足,且 event taxonomy 已稳定。
- **工程量**:L

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
| S5-06 | Capability discovery / import | 显式发现并导入 Claude/Codex/Cursor 等现有 rules、skills、MCP 配置;先提供 `ub discover` / `ub import`,不默认隐式加载所有来源 |
| S5-07 | Extension catalog | S4-10 contract 稳定且出现社区扩展后,再评估本地 catalog/marketplace;不在接口稳定前固化分发协议 |

---

## §6 研究产出(持续)

工程进展之外应该产出**可被引用的内容**,这是学习/研究向项目的核心交付。

### S6-01 博客系列:ub 内部解析

每个核心模块写一篇 ~3000 字解析:

1. Agent loop in 200 lines(`internal/app/ub/agent/loop.go` 拆解)
2. 把工具用权限包起来(`internal/pkg/runtime/permission`)
3. 流式 Bubble Tea + channel 模型(`internal/app/ub/tui`)
4. 用 rollout 事件日志做 agent 调试(`internal/pkg/workspace/rollout`)
5. Token 估算与自动压缩(`internal/pkg/llm/context`)
6. 用 fake provider 做确定性 agent 测试(`internal/pkg/llm/provider/fake`)
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

按当前 v0.4.3 基线,后续节奏重新规划如下:

### 已完成基线(v0.2.0 ~ v0.4.3)
- v0.2.x:完成 §1 收尾债务和 §2 大部分 UX 储备,包括 release/Windows/模板/doctor/session/job/MCP/clipboard/search/toast 等。
- v0.3.x:完成 §3 主体能力,包括 hooks、memory、subagents、plan/todo、multiedit、streaming、LSP 扩展、tool_result、spillover、prompt harness、ask、web、model-initiated plan mode、full-access。
- v0.4.x:围绕真实使用做体验和稳定性收敛,包括启动性能、SQLite busy、compacting 状态、textarea、ask wizard、运行中引导/排队、关键日志、内存分析、goal mode、session workspace 列。

### v0.5 Context & Prompt Reliability
目标:把"模型能不能稳定做对"从经验问题变成可观察、可测试、可调优的系统问题。

- S4-06 Prompt builder 分层:第一切片已完成 section registry、稳定顺序、manifest 测试和 `ub prompt inspect`;后续把 summary/tool guidance 纳入可检查边界。
- 上下文窗口预测:第一切片已完成 ContextWindowResolver、source/confidence、provider endpoint/model 隔离缓存和 usage/overflow 学习；显式 `max_context_tokens` 保持最高优先级，缓存失败安全回退。
- S4-08 ContextDecision + semantic pruning:统一 threshold/overflow/incomplete/mid-turn 决策,让 summary/compact 明确保留 goal、plan/todo 状态、当前阻塞、已验证事实、用户纠正和剩余风险;继续保持完整 user-turn 和 tool-call/result 边界。
- S4-07 最小 CachePlan:先锁定可缓存 section 的稳定性与 usage 解析,再为 Anthropic/OpenAI-compatible 增加 provider-specific cache 策略。
- §6-03 eval MVP:至少覆盖 read-vs-ls、先读后改、失败测试不报成功、复杂任务更新 todo/plan、压缩后继续任务这类 harness 行为。

### v0.6 Observability & Eval
目标:能解释一次 agent turn 为什么慢、为什么继续、为什么停止、为什么调用某个工具。

- S4-02 事件总线:先定义 agent event taxonomy 和 typed subscriber,把 rollout/TUI/log/tool-output 订阅边界收拢。
- S4-03 tracing MVP:为 turn、provider request、tool call、summary、memory extraction、goal continuation 记录 span;先支持本地调试,再接 OTel exporter。
- 本地诊断入口:增加 `ub trace show` / `ub stats` / `ub debug bundle` 的最小集合,优先复用 rollout/event subscriber,不另建不可回放的日志协议。
- Eval 报告:从 rollout 汇总工具调用模式、首次工具延迟、审批/工具/compact 耗时、失败恢复路径、ContextDecision 与 token/cache 经济。
- Internals 文档:补 `docs/internals/rollout-format.md`、`adding-a-tool.md`、`adding-a-provider.md` 中最常被贡献者问到的路径。

### v0.7 Session, Provider & Protocol Experiments
目标:在不破坏 rollout replay 和 session portability 的前提下探索会话分支、artifact 与 provider 新能力。

- S4-09 artifact reference MVP:为 tool spillover 增加稳定 `artifact_id`,统一解析、权限与清理边界。
- S3-16 session fork MVP:支持从历史 turn 派生子 session,为同 checkpoint 的跨 provider/model 对比提供可回放样本。
- S4-04 provider 抽象升级:评估 OpenAI Responses API/provider-side state 是否能和 ub 的 stateless rollout 语义共存。
- 统一 thinking/reasoning 展示与历史回灌边界,避免把 `reasoning_content` 当下一轮 prompt 输入。
- 结构化输出/JSON mode/function calling 抽象,只在能保留 provider-neutral tool contract 时推进。
- Anthropic/OpenAI-compatible prefix cache live probe 文档化,把 cache 命中率纳入 eval/report。

### v0.8 ~ v1.0 Hardening
目标:减少长期使用中的不确定性,为 v1.0 固化稳定边界。

- 关闭或明确延期仍未被真实使用证实的能力点。
- 收敛配置/schema/docs,梳理 breaking changes,补充 migration notes。
- 对 goal/memory/context/permission/rollout 做长期会话压力测试。
- MCP runtime hardening:固化 server 状态机、重连 backoff、tool-list 原子刷新、doctor/TUI 状态和统一 teardown;OAuth/registry 等到远程 MCP 需求出现后再做。
- 决定是否把 eventbus/tracing/provider cache 作为 v1.0 必备,还是保留为实验特性。

### v1.0 之后
- §5 生态拓展(视社区信号):IDE/Web/多人协作/SWE-bench 等。
- S4-10 extension host 与 S5-06 capability discovery:只有 hooks/MCP 无法满足的真实扩展需求出现后启动;先提供显式 inspect/import,不默认加载所有外部 agent 配置。
- S4-05 sidecar tool process:只有当隔离、协议统一或第三方工具生态成为真实痛点时再启动。

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

1. **窗口学习生命周期**:第一切片已确定 state 派生缓存与“显式配置最高优先级”;后续是否增加 TTL、provider live probe、`ub context inspect` 和手动清理入口?
2. **压缩策略**:summary 应如何保留 goal、plan/todo、memory 写入候选、用户纠正和验证状态?压缩失败时应降级为更小窗口还是直接暴露错误?
3. **Prompt section 边界**:哪些 section 属于稳定 cache prefix,哪些必须保持动态?用户可覆盖的是内容、开关、顺序,还是只允许配置预算?
4. **Eventbus vs tracing 顺序**:先做 typed eventbus 再挂 OTel,还是直接以 tracing span 反推事件 taxonomy?
5. **Provider state**:OpenAI Responses API / Anthropic provider-side memory 是否值得引入?如果引入,如何保证 rollout replay、session portability 和隐私边界?
6. **Eval 任务集**:自建小任务优先,还是引入 SWE-bench-lite 子集?如何避免 eval 反向绑死 prompt 文案?
7. **Subagents UI**:当前 parent-visible trace 足够,还是需要并列 pane / 嵌套展开 / 标签切换?这个问题应等多子 agent 并发成为真实需求后再定。
8. **Session fork 语义**:fork 应复制 rollout event、创建共享不可变前缀,还是只保存 parent/fork point 并在读取时重建?父 session 删除后子 session 如何保持可恢复?
9. **Artifact identity**:artifact ID 应只在 session 内唯一,还是使用全局 content hash?plan/tool output/subagent report 是否共享一个 store,还是只共享 resolver/lifecycle 接口?
10. **Extension trust boundary**:第三方扩展可以只读事件、注册工具,还是可以修改/阻断调用?进程外 RPC、MCP 与 WASM 分别承担什么角色?

每个开放问题在对应条目启动前,通过 `openspec/changes/` 单开一个 change 讨论。

---

## 与 V1 路线图的关系

- [`roadmap.md`](roadmap.md) (V1)已 feature-complete,作为历史存档不再更新
- V1 的 35 个迭代(I-01 ~ I-35)对应今天 ub 的全部基础设施
- V2 是在 V1 基础设施之上的演进,不重复 V1 已经做过的事
- V1 的"垂直切片优先"、"测试即验收"等原则在 V2 继续生效
