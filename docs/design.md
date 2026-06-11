# ub — 设计选型文档

> 状态：v0.1 — 与 `requirements.md` 配套。模块名暂定，实现时可微调。

## 1. 总体架构

```
                ┌────────────────────────────────────────────┐
                │                TUI Layer                    │
                │  Bubble Tea models: chat / diff / modal     │
                └───────────────┬────────────────────────────┘
                                │ events / commands (channel)
                ┌───────────────▼────────────────────────────┐
                │                App / Coordinator            │
                │  事件总线、session 调度、对外 API（预留）       │
                └───────────────┬────────────────────────────┘
                                │
        ┌───────────────────────┼────────────────────────────┐
        │                       │                            │
┌───────▼────────┐    ┌─────────▼─────────┐    ┌─────────────▼──────┐
│  Agent Loop    │    │  Tool Registry    │    │  Provider Layer    │
│  prompt 构造    │◄──►│ local + MCP tools │    │ anthropic / openai │
│  tool 调度     │    │ schema / risk     │    │ openai-compat      │
│  ctx 管理      │    └─────────┬─────────┘    └────────────────────┘
└───────┬────────┘              │
        │                       │
┌───────▼────────┐    ┌─────────▼─────────┐    ┌────────────────────┐
│ Rollout / Sess │    │  Permission       │    │   LSP Client       │
│ SQLite append  │    │  always-rules     │    │   per-language     │
└────────────────┘    └───────────────────┘    └────────────────────┘
```

**核心数据流**：
1. TUI 把用户输入封成 `UserMessage` 事件发给 App
2. App 把事件写 Rollout，转交 Agent
3. Agent 构造 prompt（runtime environment + system/summary + history + user），调 Provider
4. Provider 流式返回 `AssistantDelta` 与 `ToolCallRequest`
5. 工具调用经 Permission 过滤后由 Tool Registry 派发执行
6. `ToolResult` 进入 Rollout，回到 Agent 决定是否继续 loop
7. 终态消息流式推回 TUI

## 2. 模块划分（Go packages）

```
ub/
├── cmd/ub/main.go                     # 二进制入口
├── tools/gen-schema/                  # 仓库维护工具,不作为用户命令发布
├── api/
│   └── config.schema.json             # 配置 JSON Schema
├── internal/
│   ├── app/ub/                        # ub 应用层: CLI / agent / TUI
│   │   ├── cli/                       # cobra 子命令：run / rollout / config / sessions
│   │   ├── agent/                     # agent loop、prompt 模板、summary
│   │   └── tui/                       # Bubble Tea models 与 view
│   └── pkg/                           # ub 内部共享库,仍受 Go internal 可见性保护
│       ├── core/                      # 配置、消息模型、执行模式等基础类型
│       │   ├── config/                # YAML 加载、profile 覆盖、schema
│       │   ├── execution/             # execution mode、mode gate、mode switch events
│       │   ├── message/               # provider-neutral message model
│       │   └── reasoning/             # reasoning effort 枚举与校验
│       ├── llm/                       # provider、上下文估算与 LLM 测试支撑
│       │   ├── provider/
│       │   │   ├── provider.go        # Provider 接口、ProviderCaps、ModelInfo
│       │   │   ├── anthropic/         # 包 anthropic-sdk-go
│       │   │   ├── openai/            # 包 openai-go
│       │   │   ├── compat/            # OpenAI 兼容（DeepSeek / Together / vLLM / Ollama /v1 …）
│       │   │   └── fake/              # 单测用：脚本驱动，无 IO
│       │   ├── context/               # token 估算、压缩 / summary 策略
│       │   ├── modelinfo/             # model capability 展示与合并
│       │   └── vcr/                   # LLM 请求录制 / 回放
│       ├── runtime/                   # 运行时策略、审批、日志与启动维护
│       │   ├── permission/            # 风险判定、always-rules、UI 回调
│       │   ├── approval/              # auto 模式下的命令审批 agent
│       │   ├── hook/                  # hook runner
│       │   ├── log/                   # slog 初始化与 rotation
│       │   └── maintenance/           # 启动期清理任务
│       ├── workspace/                 # 本地状态、路径、会话与持久化
│       │   ├── paths/                 # XDG 与项目路径解析
│       │   ├── store/                 # SQLite 封装
│       │   ├── rollout/               # 事件类型、append writer、reader
│       │   ├── memory/                # workspace memory
│       │   ├── filehistory/           # 文件快照与 rewind 支撑
│       │   └── tooloutput/            # 大工具输出落盘/摘要
│       ├── integration/               # 外部协议客户端
│       │   ├── mcp/                   # MCP client（stdio/http/sse）
│       │   └── lsp/                   # LSP client
│       └── tool/
│       │   ├── tool.go                # Tool 接口、Risk、Registry
│       │   ├── fs/                    # read / write / edit / multiedit / ls / glob / tool_result
│       │   ├── plan/                  # plan_write / plan_update / plan_update_step
│       │   ├── todo/                  # todo_write / todo_update
│       │   ├── search/                # grep / glob
│       │   ├── shell/                 # bash
│       │   ├── job/                   # job_run / job_output / job_kill
│       │   └── mcp/                   # MCP tool adapter
├── docs/
├── .references/                       # 不入 git
└── go.mod
```

`internal/` 之外不暴露 API。未来若要拆 client/server，把 `internal/app/ub` 的协调层抽到独立 server package，TUI 通过 HTTP/Unix socket 接入。

## 3. Agent Loop 设计

```go
// internal/app/ub/agent/agent.go
type Agent struct {
    provider provider.Provider
    tools    *tool.Registry
    perm     *permission.Manager
    mode     execution.Mode
    rollout  *rollout.Writer
    ctx      *ctxmgr.Manager
    models   Models // large + small
}

func (a *Agent) Run(ctx context.Context, sess *session.Session, userMsg message.User) error {
    a.rollout.AppendUser(userMsg)

    turn := 0
    for {
        if maxTurns > 0 && turn >= maxTurns {
            // TUI 可通过 LimitAsker 给一次额外预算；无批准时进入 no-tools 收尾。
            return a.finalizeWithoutTools(ctx, sess.ID, messages, "tool loop reached max_turns")
        }

        // 1. 准备上下文（含 summary）
        msgs, err := a.ctx.Prepare(sess.History(), a.models.Large.MaxContext)
        if err != nil { return err }

        // 2. 调 LLM（流式）
        stream, err := a.provider.Chat(ctx, provider.Request{
            Model: a.models.Large,
            Messages: msgs,
            Tools: a.tools.Schemas(),
        })
        if err != nil { return err }

        // 3. 消费流：边收 delta 边推送 UI、收集 tool calls
        result, err := a.consumeStream(ctx, stream)
        if err != nil { return err }
        a.rollout.AppendAssistant(result.Message)

        // 4. 若没有 tool call → 终止
        if len(result.ToolCalls) == 0 { return nil }

        // 5. 顺序执行 tool calls（带执行模式与权限审批）
        toolResults := make([]tool.Result, 0, len(result.ToolCalls))
        for _, call := range result.ToolCalls {
            toolResult, err := a.runTool(ctx, call)
            if err != nil {
                // tool 错误回灌给模型，让它处理
            }
            toolResults = append(toolResults, toolResult)
        }
        turn++

        if a.loopDetector.Record(result.ToolCalls, toolResults) {
            return a.finalizeWithoutTools(ctx, sess.ID, messages, "repeated tool loop detected")
        }
    }
}
```

要点：
- **最大 turn 数**：默认不按固定步数截断；只有配置 `max_turns > 0` 时才启用 hard guard。TUI 触顶时可通过 `LimitAsker` 追加一段预算，否则 agent 会发起一次禁用工具的收尾请求。
- **执行器生命周期**：`Agent` 是轻量执行器,不是长期状态容器。TUI/headless runner 可以按用户 turn 构造新的 `Agent`,并把 `session_id`、`turn`、`history`、`context_history`、rollout writer、permission manager、tool registry 等外置状态注入进去;不要用 agent 对象池承载对话状态。CLI runtime 使用进程内 provider cache 复用同一 provider config 对应的 provider/client,并用 `agent.Factory` 从共享 Options 模板创建新的主/子 Agent。
- **并行 tool call**：V1 顺序执行，V2 再考虑并行（要保证写操作串行）
- **loop detection**：内置基础重复检测；最近窗口内相同 tool-call/result 签名重复超过阈值时，agent 不再继续调用工具，而是发起一次禁用工具的收尾请求。更复杂的跨模式/跨会话策略放到 V2 深化。
- **取消**：`ctx` 由 TUI 的 Ctrl+C 触发 cancel，provider stream 中断
- **执行模式**：`mode` 从 CLI / profile / config 注入，影响 write/exec tool 的放行路径；TUI 内模式切换只影响当前进程，不写入 session/rollout
- **活动流**：Agent 对 provider reasoning、tool lifecycle、permission decision 产生结构化 activity 事件。reasoning 只透传 provider 返回的可展示摘要（Anthropic thinking、OpenAI-compatible `reasoning_content` / `reasoning` / `thinking` 等），不伪造隐藏思维链；TUI 将同一轮连续 reasoning delta 合并成一个可展开 thinking 区域，tool lifecycle 与 permission decision 合并到独立的 tool 区域，两个区域可分别折叠/展开。TUI 参考 opencode 的降噪思路：同一 tool call 用 `tool_use_id` 原地更新，默认只显示动作短标题，工具结果细节不展开到聊天区；`todo_write` / `todo_update` 例外,它们的 tool block 只保留审计摘要,TUI 从稳定 `## Todo` result 抽取并维护一个独立 Todo checklist 区块；同一 session 内 `todo_update` 原地刷新当前 checklist,新的 `todo_write` 视为替换清单并把 standalone checklist 移到当前工具事件附近；`task` 子 agent 没有独立 session,但其 start/done、tool lifecycle、permission 等 display-only activity 会镜像到父 session/turn,携带 `parent_tool_use_id`、`subagent_id`,并把 child tool id 命名空间化为 `subagent:<parent-task>:<child-tool>`;TUI 用 `subagent:` 前缀展示并在恢复 session 时重建这些 activity。子 agent reasoning delta 只走 live activity,不逐片持久化。所有内置工具和 `mcp__<server>__<tool>` 动态工具都必须有稳定可读的 running/done 标签，避免退回不明确的 `Working...`；展开 tool 区域后先展示每个工具的摘要，带详情的 write/edit/multiedit 工具项可通过活动焦点展开 colored unified diff；read/ls/glob/bash/task/MCP 等无文件 diff 的工具展开时按普通文本展示限幅后的 tool result 内容，不把 markdown 列表或普通 `+` / `-` 行当作 diff 着色；bash 会把 `<shell_metadata>` 转成普通 metadata 行；streaming partial output 在最终 done/failed 到达时不被空详情或仅 metadata 的详情覆盖；activity 层再次限幅详情时必须显示 `activity detail truncated` 提示，并在存在 tool result truncation footer 时保留 `full_output_path`；展开详情被当前 TUI 视窗裁剪但数据本身未限幅时，消息区底部显示 `[tool detail clipped: ...]` 提示。恢复 session 时，TUI 从 rollout `tool_result` payload 重建相同的摘要和详情，保留 files/truncation metadata，而不是只从 message history 的纯文本 tool_result 回放。
- **TUI 消息队列**：同一 session 内 Agent turn 仍保持串行。运行中用户输入普通消息并回车时，TUI 只写入本地 FIFO 队列，不并发调用 Agent；当前 stream 正常关闭后自动取队首启动下一轮。排队消息在真正启动前不写入 rollout，避免被中断或编辑后的草稿污染历史；运行中上下方向键优先进入队列编辑，再退回普通历史输入浏览。`/btw [question]` 是队列例外：TUI 立即启动独立旁路 provider 请求，不取消当前 Agent turn，也不把问题放入主队列。
- **TUI 启动覆盖**：直接运行 `ub` 打开 TUI 时支持 `--provider <name>` 与 `--model <id>`，走与 `ub chat` 相同的 provider/model 选择规则，只影响本次启动，不写回配置。
- **TUI provider 切换**：`/provider [provider] [model]` 在当前 TUI session 内切换后续主对话 provider；无参数时展示 provider picker，显式切换后刷新 model/effort 候选与状态栏。切换只写回当前 session 元数据，不写回配置；不指定 model 时优先保留目标 provider 可用的当前 model。
- **TUI session 恢复**：`ub --resume` 不再静默选择最近 session，而是在启动后打开当前 workspace 的历史 session picker；`ub --resume=<id>` / `ub --resume <id>` 仍在进入 TUI 前直接恢复指定 session。TUI 内 `/resume` 对齐 CLI resume 语义：无参数打开 session picker，带 session id 时直接恢复；`/sessions` 继续负责 session picker、直接切换和 `search <query>` 历史事件搜索。恢复时同时还原 session 元数据中的 provider 与 model；旧 session 若缺少 provider，会先按配置/远端模型列表尽力推断。
- **TUI rewind**：`/rewind` 从当前 session 的 rollout events 中列出历史 `user_message`，打开可筛选 picker；选中某条后删除该 turn 及之后的 events，重建 runner history、TUI transcript、下一 turn 和 context 状态，并把该 user message 放回输入框。`/rewind <turn>` 可直接定位目标 turn。文件回退采用 Claude-style checkpoint：Agent 在每个 user turn 开始前写入 `file_history_snapshot` event，并把已跟踪文件的旧内容备份到 state root；`write` / `edit` / `multiedit` 和可安全解析为字面路径的 `bash` 删除（`rm` / `git rm`）会在真正执行前补齐当前 turn 的文件旧状态。picker 根据当前 workspace 与目标 checkpoint 做 dry-run：默认只回退对话并保留 workspace 文件，也可选择同时把已跟踪文件恢复到目标 user message 之前的状态。变量、通配符、命令内 `cd` 等不可靠 shell 路径不会进入文件历史；checkpoint 中没有可靠旧状态的文件会跳过并在 TUI 提示。
- **TUI 本地输入增强**：首个非空字符为 `!` 的输入绕过 Agent，输入区显示 shell 模式提示，直接复用本地 `bash` 工具执行并只在当前 TUI 以本地输出展示结果，不写入 rollout/history、不走权限审批、也不渲染为模型 tool 调用；`/btw [question]` 切换到独立的内存 BTW 视图，带问题时复用当前 provider/model/reasoning 与无工具 runtime context，追加当前 session text-only history、旁路系统提示和 BTW 视图内已完成 Q/A 后发起一次 `Tools=nil` 的 provider 请求，流式答案只写入 BTW 视图内存并复用普通助手消息的 Markdown renderer，任何 provider tool call 或伪工具调用标记都按错误处理；BTW 视图内输入文字并回车会继续追问，只把视图内 Q/A 作为临时 side history，不写入主 rollout/history；BTW 长输出使用独立滚动状态，不滚动主聊天区；底部状态行显示 BTW 视图、模型、状态（`answering` / `idle`）和退出提示，不展示主对话的 context/cwd/help 状态栏；`Esc` 退出 BTW 视图并清空该视图的临时 Q/A、草稿和滚动位置；普通输入中的 `@prefix` 触发 workspace 文件候选，选择后插入 `@relative/path` 文本引用，不自动读取文件内容；输入组件关闭 virtual cursor，由每帧 `tea.View.Cursor` 暴露输入框真实光标，保证 IME 预编辑绘制在当前输入行。

## 4. Tool 系统

```go
// internal/pkg/tool/tool.go
type Tool interface {
    Name() string
    Description() string
    Schema() jsonschema.Definition
    Risk() Risk                              // safe / write / exec
    Execute(ctx context.Context, args json.RawMessage) (Result, error)
}

// 可选接口：写类工具实现它，dispatcher 会在 Execute 前调一次 Preview，
// 把结果交给 permission UI 渲染（diff / 摘要），用户确认后再 Execute。
// 这样 model 不需要感知 dry_run，调用仍是单步。
type PreviewableTool interface {
    Tool
    Preview(ctx context.Context, args json.RawMessage) (Preview, error)
}

type Preview struct {
    Summary string                           // 一行人类可读摘要，例如 "Edit main.go: 1 replacement"
    Files   []FileDiff                       // 文件级 diff，给 TUI diffview 渲染
}

type FileDiff struct {
    Path        string
    Kind        string                       // "create" / "modify" / "delete"
    UnifiedDiff string                       // 标准 unified diff 文本
}

type Result struct {
    Content string                           // 文本结果（回给模型的 tool_result）
    IsError bool
    Files   []FileChange                     // 执行后的实际改动摘要（可与 Preview 不完全一致，例如并发修改）
}

type FileChange struct {
    Path        string
    Kind        string                       // "create" / "modify" / "delete"
    UnifiedDiff string                       // 可选；write/edit Execute 会带上实际写盘 diff，供 TUI 展开详情
}
```

**风险等级**：
- `safe`：read / ls / grep / glob / diagnostics / references / hover / completion / document_symbols / rename / code_action / tool_result / plan_write / plan_update / plan_update_step / todo_write / todo_update / remember / recall / task
- `write`：write / edit / multiedit
- `exec`：bash / job_run / job_kill

`plan_write` / `plan_update` / `plan_update_step`:把 plan-then-execute 工作流落到磁盘。plan 模式产出 `$XDG_STATE_HOME/ub/plans/<project-key>/<id>.md`(标题、metadata、`## Steps` 任务列表、`## Notes`、`## Log`),TUI 在 `plan_write` / `plan_update` 完成摘要中直接展示 `plan_id`。用户纠正已有计划时用 `plan_update` 原地更新同一个 artifact;也可在 TUI 中通过 `/plans` 打开当前 workspace 的 plan picker,或通过 `/plan-edit <plan-id>` / `/plans <plan-id>` 用 `$VISUAL` / `$EDITOR` 直接打开该 markdown review/edit。work/auto 模式按这个 artifact 推进并 `plan_update_step` 标记每一步。`plan_write` / `plan_update` 只在 plan 模式暴露和执行;`plan_update_step` 只在 work/auto 模式用于执行进度。plan 模式只向 provider 广告 `read` / `ls` / `glob` / `grep` / `plan_write` / `plan_update`,误调用其它工具仍由 mode gate 拦截。三者都是 `RiskSafe`(写的是 ub 用户 state artifact 目录,不是用户代码)。

`todo_write` / `todo_update`:维护当前 session 的短生命周期执行清单,不复用 plan markdown checkbox。`todo_write` 创建或替换当前清单,`todo_update` 通过 `id` 或 1-based `item_index` 更新单项状态。状态为 `pending` / `in_progress` / `completed` / `skipped` / `failed`,并约束同一清单最多一个 `in_progress`。todo state 存在 `$XDG_STATE_HOME/ub/todos/<session-id>.json`,tool result 同时输出稳定的 `## Todo` 文本;TUI 将该文本抽取成独立 Todo checklist,rollout show 和 resume 也能恢复同一份执行视图。todo 工具只在 work/auto/full-access 执行阶段暴露;plan 模式不广告也不执行。

LSP 工具家族(全部 `RiskSafe`):`diagnostics` / `references` 之外,新增 `hover`、`completion`、`document_symbols`、`rename`、`code_action`。其中 `rename` 与 `code_action` **只返回 LSP 的建议**,不直接落盘 —— rename 输出"按文件路径排序的边界列表",model 拿到后用 `multiedit` 自行应用,从而走 ub 的 preview/permission 协议;`code_action` 只列可用 action 的 `title (kind)[ — has_edit]`,不执行任何 action

`tool_result(tool_use_id, offset?, limit?)`：从 `<state-root>/tool_outputs/<sessionID>/<toolUseID>.txt` 读回曾被 `tooloutput.LimitResult` 截断/落盘的完整工具输出。sessionID 由 agent 调用前通过 `tool.WithSessionID(ctx)` 注入到 context；工具自身不接受任意路径,只能读 spillover 目录,跨 session 不可见

**Registry**：本地工具静态注册，MCP 工具运行时注册。同名冲突时 MCP 走 `mcp__<server>__<tool>` 前缀（Anthropic 规范）。

**Streaming tools**：工具可选择实现 `StreamingTool` 接口(继承 `Tool` 之上多一个 `ExecuteStream(ctx, args, events chan<- StreamEvent)`)。agent runtime 检测到该接口时,在新 goroutine 跑 `ExecuteStream`,把每条 `StreamEvent{Kind,Data}` 转成 `EventToolPartialOutput` 推到 EventSink,让 TUI 在工具未结束前看到滚动预览。`bash` 已接入 stdout/stderr streaming;`job_output` 在 `follow=true` 时先推当前 ring buffer 快照,再追踪新增 stdout/stderr,直到 job 退出、`timeout_ms` 到期或请求取消。其他工具仍同步 `Execute`,不发 partial 事件。chunk 在 emit 前截到 4KB。

**两阶段调用流程**（仅 PreviewableTool）：
```
1. agent 解析 tool_call
2. tool := registry.Get(name)
3. mode gate:
     - plan + write risk: 拒绝，回灌 ToolResult{IsError, Content="plan mode is read-only"}
     - work / auto / full-access + write risk: 继续
4. if pt, ok := tool.(PreviewableTool); ok:
       preview = pt.Preview(ctx, args)
5. exec risk:
     - allow-rule match: 直接 Allow（黑名单除外）
     - work / plan: permission.AskHuman(call, preview)
     - auto: permission.AskApprovalAgent(call)；拒绝 / 不确定 / 错误时回退 AskHuman
6. if decision != Allow: 回灌 ToolResult{IsError, Content="denied"} 给模型
7. else: result = tool.Execute(ctx, args)
```

**关键 tool 实现要点**：
- `edit` / `write`：实现 `PreviewableTool`。Preview 读现盘 + 在内存里应用 patch + 用 `go-udiff` 算 unified diff；Execute 实际写盘，并在 `FileChange.UnifiedDiff` 中返回实际变更。`edit` 默认用 `old` / `new` 精确子串替换，`old` 必须逐字节匹配；当模型难以从带行号的 `read` 输出复原 tab、空格或换行时，可用 `start_line` / `end_line` 替换完整行，仍通过同一 preview / permission / TOCTOU 路径。TUI 默认只展示摘要，按 `Ctrl+O` 展开最近的 tool 区域后先展示工具摘要，再按一次展开最近工具项的着色文件级详情；也可用 `Ctrl+N` / `Ctrl+P` 移动活动焦点并用 `Enter` / `Space` 操作任意活动块或工具项；TUI 默认不启用鼠标追踪，保留终端原生拖拽选择复制。`multiedit`（一次调用跨文件多处编辑）共用 `applyEdit` 与 `udiff`，在内存中按数组顺序对同 path 串行累加，先对所有目标做 TOCTOU 二次读校验再批量写盘，任一步失败即不写盘（写过的文件回滚到 before 快照），从而对调用方提供 all-or-nothing 语义
- `bash`：用 `os/exec` 拉子进程，stdout/stderr 流式回传；超时默认 120s；不实现 Preview（命令是黑盒）
- `job_run`：返回 `job_id`，进程交给后台 goroutine 管理；`job_output(job_id, tail?)` 返回当前 stdout/stderr 快照；`job_output(job_id, follow=true, timeout_ms?)` 通过 `StreamingTool` 推送当前快照和新增输出,最终仍返回同一快照格式；`job_kill` SIGTERM/SIGKILL
- tool 参数解析对模型常见 JSON 标量抖动做窄容错：整数参数接受整数或整数字符串，布尔参数接受布尔值或 `"true"` / `"false"`，但 JSON Schema 仍对外声明真实 integer/boolean 类型
- `references` 优先支持 `symbol` + 可选 `path` 的符号名查询，由本地搜索定位候选位置后再调用 LSP；兼容 `file` + `line` + `col` 的位置查询

## 5. Provider 抽象

```go
// internal/pkg/llm/provider/provider.go
type Provider interface {
    Name() string
    Chat(ctx context.Context, req Request) (Stream, error)
    Caps() Caps
}

type Caps struct {
    SupportsTools       bool
    SupportsStreaming   bool
    SupportsPromptCache bool
    MaxContextTokens    int
    SupportsVision      bool
}

type ModelInfo struct {
    ID       string
    Provider string
    Caps     Caps
    Price    Price  // 可选：输入/输出 per-1M-token
    SupportsReasoning bool
    SupportedEfforts  []ReasoningEffort
    DefaultEffort     ReasoningEffort
}

type Stream interface {
    Next(ctx context.Context) (Event, error)  // text delta / reasoning delta / tool call / usage / done
    Close() error
}
```

**双层抽象**（借鉴 codex-rs 的 `model-provider` + `model-provider-info`）：
- `Provider` 是行为接口
- `ModelInfo` 是元信息（含 Caps），存配置文件 + 内置默认表
- reasoning 能力按 `用户配置覆盖 > 内置 ModelInfo 表 > 保守未知模型` 解析；未知模型默认不发送 reasoning 参数

**所有 provider 的统一可配置项**（不止 openai-compat）：

```go
type ProviderConfig struct {
    Type    string            // anthropic / openai / openai-compat
    APIKey  string            // 支持 ${ENV} 替换
    BaseURL string            // 可选，覆盖 SDK 默认 endpoint
    Headers map[string]string // 可选，额外 HTTP header（鉴权 / 路由）
    Timeout time.Duration     // 可选；等待响应头的超时（默认 120s）。不限制流式 body 总长
    Models  map[string]ModelConfig // 可选，覆盖模型能力
}

type ModelConfig struct {
    SupportsReasoning bool
    SupportedEfforts  []reasoning.Effort
    DefaultEffort     reasoning.Effort
    MaxContextTokens  int // 可选，模型级 context window 覆盖
}
```

构造 anthropic / openai 客户端时把 `BaseURL` 传给官方 SDK 的 option（`anthropic.WithBaseURL` / `openai.WithBaseURL`），都是 SDK 直接支持的。用途：LiteLLM、Cloudflare AI Gateway、Helicone、OneAPI、公司内部反代、企业代理。

**reasoning effort**：
- `provider.Request` 携带可选 `ReasoningConfig{Effort, Summary}`，Agent 在发送前按当前模型能力校验
- OpenAI / OpenAI-compatible 映射为 `reasoning_effort`；未知兼容模型默认不发送
- Anthropic 映射为 `thinking` budget，`none` 不发送 thinking，非 `none` 时自动保证 budget 小于 `max_tokens`
- TUI 通过 `/effort` 列出和切换当前模型支持的等级，并在状态栏展示当前值

**上下文压缩与状态栏**：
- Agent 每次发起 provider 请求前估算请求消息 token（含 tool schema），并通过 runtime event 向 TUI 上报 used/max/%；TUI 状态栏用 `ctx est` 展示请求前估算，用 `ctx last` 展示 provider usage 的最近实际 input token
- Agent 每次 provider 请求都会临时注入当前运行环境（workspace cwd、shell、OS）和路径规则，避免模型猜测 `/home/user` 等默认路径；该 runtime context 不写入 rollout，也不进入恢复后的历史消息
- max context 优先读取 `providers.<name>.models.<model>.max_context_tokens`，未配置时回退 provider `Caps().MaxContextTokens`
- 自动 summary 按 `estimated_input + context.reserve_output_tokens > max_context * context.trigger_ratio` 触发；summary 默认使用当前主对话模型，避免把决定后续上下文的高风险压缩交给小模型。如果 provider 仍返回可识别的上下文超限错误，Agent 会强制执行一次同一 summary 策略并重试同一轮请求，重试后仍失败则返回 provider 原始错误；TUI 的 `/compact` 可主动触发同一 summary 逻辑。最近原文保留使用 `context.keep_recent_turns` + 最近上下文 token budget，至少保留当前 user turn，且按完整 user turn 边界截断，避免孤立 tool_use/tool_result
- summary prompt 自身也按 summary 模型的 context window 做预算控制：待摘要 conversation 超过预算时，Agent 只按完整 user turn 边界打包分块，再把块摘要递归合并，避免主模型已超限后 summary 模型继续收到同样超限的历史；单个 user turn 自身超预算时不做 message/字符级切碎，直接返回明确错误
- tool result 在进入下一次 provider 请求和写入 rollout 前统一限幅：默认最多 12KiB/400 行模型可见内容；超限时完整输出写入 `$XDG_STATE_HOME/ub/tool_outputs/<session>/<tool_use>.txt`（否则 `~/.local/state/ub/...`），rollout 只保存 preview、`truncated`、`original_bytes`、`full_output_path`
- 读取 rollout 历史时保留完整可见 transcript，`Summary` 事件不作为用户/助手消息渲染；同时为 provider 请求单独构造 context history，遇到 `Summary` 事件即用事件中记录的 compacted messages（summary system message + 保留的最近原文窗口）替换请求上下文，避免恢复 session 后重新带上已压缩旧消息；旧 `Summary` 事件没有 compacted messages 时退回只使用 summary system message

**重要的内部消息表示**：
不要直接复用 anthropic / openai 的请求类型。在 `internal/pkg/core/message/` 自定义中性 `Message` 结构（`Role`、`Content[]`；content block 包含 text / reasoning / image / tool_use / tool_result），各 provider 各自转换。理由：避免被某家 SDK 锁定。

## 6. Rollout（事件日志）

```go
// internal/pkg/workspace/rollout/event.go
type Event struct {
    ID        string
    SessionID string
    Turn      int
    Time      time.Time
    Type      EventType   // user_message / assistant_message / tool_result / summary / usage / activity / error
    Payload   json.RawMessage
}
```

**存储**：SQLite 表 `events(id, session_id, turn, time, type, payload BLOB)`，按 `(session_id, turn, time)` 建索引。写入策略：单条 `INSERT` 即 commit；DB 启用 `PRAGMA journal_mode=WAL` + `PRAGMA synchronous=NORMAL`。

**清理与截断**：启动时做 best-effort 自动清理，默认最多每 24h 运行一次（记录在 `$XDG_STATE_HOME/ub/cleanup.json`，否则 `~/.local/state/ub/cleanup.json`）。默认删除 30 天未更新且不属于其 workspace 最近 20 个的 session；`events` 不做自动单 session 内局部裁剪，只随 `sessions` 的 `ON DELETE CASCADE` 删除，避免破坏历史恢复、summary 和 rollout replay。删除 session（显式 `sessions rm/delete`、`sessions clear`、全局 clear 或启动过期清理）会同时清理 session 关联的 state artifacts：`$XDG_STATE_HOME/ub/todos/<session-id>.json`、`$XDG_STATE_HOME/ub/file-history/<session-id>/`、默认 tool-output spillover 目录 `$XDG_STATE_HOME/ub/tool_outputs/<session-id>/`，以及该 session 的 rollout `tool_result` 引用且未被其它 session 引用的 plan markdown 文件。唯一的单 session 局部截断入口是用户显式 `/rewind`：删除目标 turn 及之后 events 后立即重建内存 history 与 TUI 显示。tool-output spillover 文件按 `context.tool_results.spillover_max_age` 清理，失败只 warning。启动清理失败只写 warning，不阻断 CLI/TUI 主流程；默认不执行 SQLite `VACUUM`，避免启动时长时间阻塞。

**耐久性目标**（与 requirements F-SESS-4 对齐）：进程崩溃（panic / OOM / SIGKILL）不丢已 commit 的事件；操作系统断电可能丢最后若干条尚未刷盘的事件。**不**为此牺牲性能逐条 fsync——agent 一轮会写数十条事件，每条 fsync 在 SSD 上也要几毫秒，TUI 流畅度会肉眼可见地下降。

**用途**：
1. **会话恢复**：重启后 reader 把事件还原为内存中的 `[]Message`
2. **调试**：`ub rollout show <session>` 漂亮打印整轮 trace
3. **Rewind**：TUI 可基于 events 选择历史 user turn，并截断到该 turn 之前
4. **vcr 替代品**：录制真实跑过的 session 后，可在测试里重放（结合 §10）
5. **未来 audit/导出**

## 7. 会话存储

SQLite 单库 `~/.local/share/ub/ub.db`：

```sql
CREATE TABLE sessions (
    id           TEXT PRIMARY KEY,
    workspace    TEXT NOT NULL,
    title        TEXT,
    created_at   INTEGER,
    updated_at   INTEGER,
    summary      TEXT,
    provider     TEXT,
    model        TEXT
);
CREATE INDEX idx_sessions_ws_updated ON sessions(workspace, updated_at DESC);

CREATE TABLE events (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    turn        INTEGER NOT NULL,
    time        INTEGER NOT NULL,
    type        TEXT NOT NULL,
    payload     BLOB NOT NULL
);
CREATE INDEX idx_events_session ON events(session_id, turn, time);
```

考虑用 `modernc.org/sqlite`（纯 Go，免 cgo，分发友好）而不是 `mattn/go-sqlite3`。

是否上 `sqlc`？V1 先手写 SQL（4 张表以内），V2 表多了再引入。

## 8. 配置

`~/.config/ub/config.yaml` 示例：

```yaml
default_provider: anthropic
default_model: claude-sonnet-4-7   # 可省略；provider 可列模型时自动选第一个
small_model: gpt-4o-mini           # 完整模型名；当前 provider 可用时用于 auto memory、生成标题、approval fallback
execution_mode: work               # work / plan / auto / full-access

prompt:
  workspace_instructions:
    enabled: true                   # 注入 AGENTS.md
    max_chars: 12000
  git_snapshot:
    enabled: true                   # 注入启动时 git 快照；不是实时状态
    max_chars: 4000
  compact_style: structured         # short / structured

approval_agent:
  provider: openai
  model: gpt-4o-mini               # 可省略；优先 small_model，再按 provider 模型列表 fallback
  # 仅 auto 模式使用；未配置时默认复用当前 provider + small_model/default_model；
  # 失败或拒绝时记录日志并回退到用户审批

providers:
  # 所有 provider 都支持 base_url / headers / timeout 覆盖
  anthropic:
    type: anthropic
    api_key: ${ANTHROPIC_API_KEY}
    # base_url: https://my-litellm-proxy.example.com/v1   # 可选
    # headers:
    #   x-org-id: ub-dev
    # timeout: 120s

  openai:
    type: openai
    api_key: ${OPENAI_API_KEY}
    # base_url: https://gateway.ai.cloudflare.com/v1/{acct}/{gw}/openai

  deepseek:
    type: openai-compat
    base_url: https://api.deepseek.com/v1
    api_key: ${DEEPSEEK_API_KEY}

  azure-openai:
    type: openai-compat   # Azure 走 OpenAI 兼容路径
    base_url: https://my-azure.openai.azure.com/openai/deployments/gpt-4o
    api_key: ${AZURE_OPENAI_KEY}
    headers:
      api-version: "2024-08-01-preview"

  local-ollama:
    type: openai-compat
    base_url: http://localhost:11434/v1   # Ollama /v1 OpenAI 兼容端点；远端 Ollama 改这里
    api_key: ollama

  company-anthropic-proxy:
    type: anthropic        # 走自家 Anthropic 反代
    api_key: ${INTERNAL_TOKEN}
    base_url: https://llm-gateway.intra.example.com/anthropic/v1

permissions:
  allow:
    - Bash(git status)
    - Bash(ls:*)
  ask:
    - Bash(git push:*)
  deny:
    - Bash(curl:*)

mcp_servers:
  filesystem:
    type: stdio
    command: npx
    args: ["@modelcontextprotocol/server-filesystem", "${PWD}"]

lsp_servers:
  go:
    command: gopls
    file_types: [".go"]
  ts:
    command: typescript-language-server
    args: [--stdio]
    file_types: [".ts", ".tsx"]

tui:
  theme: dark
  compact: false

context:
  trigger_ratio: 0.8
  keep_recent_turns: 3
  reserve_output_tokens: 12000
  tool_results:
    inline_max_bytes: 12288
    inline_max_lines: 400
    spillover_enabled: true
    spillover_max_age: 168h

cleanup:
  enabled: true
  interval: 24h
  sessions:
    max_age: 720h
    min_recent_per_workspace: 20
  logs:
    max_size_mb: 10
    max_backups: 5
```

加载顺序：内置默认 → 全局配置 → 工作目录 `.ub/config.yaml` → 环境变量覆盖。

`tui.theme` 会传给 TUI 的 Markdown renderer；当前支持 Glamour 内置样式 `dark`、`light`、`notty`、`ascii`、`dracula`、`tokyo-night`、`pink`，未知值回退到 `dark`。

`cleanup.enabled` 区分 unset 与显式 `false`，因此用户可以在全局或 profile 中关闭默认开启的清理。日志轮转在打开 log 文件前执行：超过阈值时 `ub.log` 变为 `ub.log.1`，已有历史依次后移，超过 `max_backups` 的旧文件删除；不压缩日志，`max_size_mb <= 0` 或 `max_backups < 0` 视为关闭轮转。`UB_LOG_FILE` 与 TUI 默认日志路径走同一套轮转逻辑。

## 9. 执行模式与权限模型

`execution.Mode` 是运行时策略，不等同于 provider profile，也不随 session 持久化。profile 决定模型/配置，mode 决定 tool call 能否落地。

```go
type Mode string
const (
    ModeWork Mode = "work"
    ModePlan Mode = "plan"
    ModeAuto Mode = "auto"
    ModeFullAccess Mode = "full-access"
)

type ModePolicy struct {
    AllowWrite bool
    ExecPath   ExecApprovalPath // human / approval-agent-with-human-fallback
}
```

**四种模式**：
- `work`：允许 workspace 内文件读写；`exec` 风险工具如果没有命中 allow-rule，走用户审批。
- `plan`：只读规划；只广告 `read` / `ls` / `glob` / `grep` / `plan_write` / `plan_update`。`write` 与 `exec` 风险工具、sub-agent、memory、LSP/MCP 等其它工具在 dispatcher 层直接拒绝并把错误回灌给模型。
- `auto`：文件读写策略同 `work`；`exec` 风险工具先交给 approval agent 自动判断，若拒绝、不确定或异常，再回退到用户显式审批。
- `full-access`：文件读写策略同 `work`；`exec` 风险工具在未命中 deny/ask/黑名单时由 mode 直接放行，不走 approval agent 或用户审批。当前 TUI 切入该模式不弹首次高风险确认 dialog；风险提示依赖状态栏、帮助文案和后续 tool activity 审计。

**approval agent** 是一个受限的二级 agent，只输出 `allow` / `deny` / `unsure` 与一句理由，不执行工具、不修改上下文、不写文件。它的输入只包含命令文本、cwd、风险等级、当前 mode、最近相关上下文摘要和已命中的规则信息；API key 等 secret 不传入。黑名单命令不进入 approval agent，直接走用户确认。

```go
type Decision int
const (
    Allow Decision = iota
    Deny
    AlwaysAllowCommand   // session 内：同 tool + 同参数自动放行（内存）
    AlwaysAllowTool      // session 内：同 tool 全放行（内存）
    AlwaysAllowProjectCommand // 项目内跨 session：同 tool + 同 command 持久化
    AlwaysAllowProjectPattern // 项目内跨 session：同 tool + Claude-style Bash pattern 持久化
)

type ApprovalAgent interface {
    ReviewCommand(ctx context.Context, req Request) (ApprovalAgentDecision, error)
}
```

**两层规则存储**：
- **session 级**（`AlwaysAllowCommand` / `AlwaysAllowTool`）：内存 map，agent 进程退出即丢
- **project 级**：序列化到 `<workspace>/.ub/permissions.yaml`，启动时加载到内存；格式参考 Claude Code `settings.json` 的 permission rules
  ```yaml
  # <workspace>/.ub/permissions.yaml — 由 TUI 写入，用户也可手改
  permissions:
    allow:
      - Bash(git status)
      - Bash(go test:*)
    ask:
      - Bash(git push:*)
    deny:
      - Bash(curl:*)
  ```

UI 流程：
1. tool dispatcher 收到 call，先执行 mode gate：`plan` 模式拒绝所有 `write` 风险工具
2. 若工具实现 PreviewableTool，先调 `Preview()`
3. 对 `exec` 风险工具先查 project `deny` rules → project `allow` rules → session allow rules → project `ask` rules（黑名单除外）；`deny` 直接拒绝，`allow` 直接通过，`ask` 强制人工确认
4. 不 match 时按 mode 选择审批路径：`work`/`plan` 直接向 TUI 发 `PermissionRequest{Call, Preview}`；`auto` 先调 approval agent，并用 `slog` 记录 `allow`/`deny`/`unsure` 或错误原因，返回 `deny`/`unsure`/error 时再向 TUI 发请求；`full-access` 直接返回 mode allow
5. TUI 弹 modal，以候选列表展示 6 个选项（与 F-PERM-3 对齐），每个选项都说明作用范围；上/下方向键移动，Enter 确认，`1`~`6` 仅作为快捷键：
   - Allow once：只允许本次请求，不保存规则
   - Deny：拒绝本次请求，不保存规则
   - Always allow this exact command (session)：本 session 内允许完全相同 command
   - Always allow this tool (session)：本 session 内允许同一 tool 的后续调用
   - Always allow this exact command (project)：向 `permissions.allow` 追加 `Bash(<exact command>)`
   - Always allow this similar command (project)：向 `permissions.allow` 追加 Claude-style `Bash(<prefix>:*)`
6. 决策写入 rollout（`Activity` 事件，`activity_kind=permission`，含 `source=mode|rule|approval_agent|human`），resume 时恢复到 tool 活动区；选 3/4 更新内存 rules；选 5/6 同时追加到项目级磁盘 yaml
7. dispatcher 拿到决策继续

`Bash(pattern)` 规则采用 Claude-style prefix/wildcard 语义：无 `*` 时精确匹配；`cmd:*` 匹配该前缀的 shell 命令；compound command 会按 `&&`、`;`、管道、换行等拆分，只有每个子命令都命中 allow rule 才能自动放行，任一子命令命中 deny rule 则整条命令拒绝。

**approval 模型切换规划**：`/approval-model [model]` 只影响 auto 模式的命令审批模型，不改变主对话模型。无参数时展示 approval provider 的候选模型；显式指定时必须通过候选列表校验；切换成功后重建 `permission.Manager` 内的 approval agent，并仅影响后续 tool approval。

**small 模型切换规划**：`/small-model [model]` 只影响当前进程内 auto memory 使用的模型，不改变主对话模型、compact summary 模型，也不写回配置文件。候选来自当前 provider 的完整模型字符串列表；显式指定时必须通过候选列表校验；切换成功后后续 auto memory 使用该模型。

**auto memory 调度**：agent 成功 turn 结束时先发送 `EventDone`,再把本轮消息交给 `MemoryAutoScheduler`。调度器在前台只做低成本门控:plan 模式、空 workspace、显式 `remember` 已经写入的 turn、以及默认配置下包含 MCP / web / tool_search 等外部上下文工具的 turn 不进入自动抽取。其余消息按 `memory.auto.trigger`、累计 turn 数、累计可见消息数和最小间隔批量调度后台 small-model 抽取;正在抽取时只保留一个合并后的 pending job。TUI 复用 session 级 scheduler,不等待抽取完成;headless `ub run` 在主答案输出后按 `memory.auto.drain_timeout` 做 best-effort drain。实际写入仍走 `memory.AppendWithOutcome` 和 `memory_write` rollout 事件。

**黑名单**：硬编码的强制再确认正则（`rm\s+-rf\s+/`、`mkfs\.`、`dd\s+.*of=/dev/`）。即使任意 always-rule match 也再弹一次。

## 10. 测试策略

**单元测试**：
- `tool/*` 各工具：构造临时目录跑真实 IO
- `agent/`：用 fake provider（返回固定 stream）测 loop
- `context/`：token 估算 + 触发 summary 边界
- `permission/`：always-rules 匹配、黑名单优先级

**集成测试 + vcr**（`internal/pkg/llm/vcr/`）：
- 首次跑：真打 LLM，写入 `testdata/cassettes/*.jsonl`（脱敏 API key）
- 回放：HTTP transport 拦截，按 cassette 顺序匹配请求返回响应
- 类似 [Crush 的 charm.land/x/vcr](https://charm.land/x/vcr) 或 Ruby vcr

**端到端冒烟**：跑一个 `ub run --headless --script` 模式，按脚本投递用户消息，断言文件被正确修改。

## 11. 技术栈一览

| 关注点 | 选型 | 备注 |
|---|---|---|
| 语言 | Go 1.23+ | |
| CLI | `spf13/cobra` | 主流 |
| TUI | `charm.land/bubbletea/v2` | |
| TUI 组件 | `charm.land/bubbles/v2`、`charm.land/lipgloss/v2`、`charm.land/glamour/v2` | |
| LLM Anthropic | `anthropics/anthropic-sdk-go` | 官方 |
| LLM OpenAI | `openai/openai-go/v3` | 官方 |
| HTTP | 标准库 `net/http` + 自家 transport（含 vcr 注入） | |
| JSON Schema | `invopop/jsonschema` | tool schema 生成 |
| SQLite | `modernc.org/sqlite` | 纯 Go |
| Diff | `aymanbagabas/go-udiff` | Crush 同款 |
| Log | `slog` 标准库 | CLI 默认 stderr；TUI 默认 `$XDG_STATE_HOME/ub/ub.log` 或 `~/.local/state/ub/ub.log` |
| Config | `goccy/go-yaml` + `env` 替换 | |
| 进程管理 | `os/exec` + 自家 PG 控制 | |
| LSP | `gopls.dev/protocol` 或自家精简实现 | 仅用 diagnostics/references |
| MCP | 自家实现 client（参考 modelcontextprotocol 官方 schema） | Go 官方库不成熟时手写 |
| 测试 | `testing` + `testify/require` | |
| 录制 | 自家 vcr（内部包） | |

## 12. 开发与测试机制

针对学习/研究向项目，最大的摩擦是"想验证一个想法时不愿打真实 LLM"。提供三种互补路径：

### 12.1 fake provider（单元测试用）

- 包：`internal/pkg/llm/provider/fake/`
- 配置：
  ```yaml
  providers:
    test:
      type: fake
      script:
        - { type: text_delta, text: "Looking at the file...\n" }
        - { type: reasoning_delta, reasoning: "checking which file to read" }
        - { type: tool_call, name: "fs.read", input: {path: "main.go"} }
        - { type: text_delta, text: "Found 3 functions." }
        - { type: done }
  ```
- 也可在 Go 代码里直接构造（推荐用于单测）：
  ```go
  p := fake.New(fake.Script{
      fake.TextDelta("hi"),
      fake.ReasoningDelta("checking context"),
      fake.ToolCall("fs.read", map[string]any{"path": "x"}),
      fake.Done(),
  })
  ```
- 用途：覆盖 agent loop / tool dispatcher / permission 流程的确定性测试
- 与 vcr 的区别：vcr 重放**真实历史请求**，验证 provider 适配层；fake 模拟**任意期望行为**，验证编排层

### 12.2 dev profile（实时开发用）

配置 `profiles:` 节：

```yaml
default_provider: anthropic
default_model: claude-sonnet-4-7

providers:
  vllm-local:
    type: openai-compat
    base_url: http://localhost:8000/v1
    api_key: dummy

profiles:
  dev:
    default_provider: vllm-local
    default_model: Qwen2.5-Coder-7B-Instruct
    small_model:   Qwen2.5-Coder-7B-Instruct
    execution_mode: auto
    permissions:
      auto_allow_safe: true
      auto_allow_write: true   # 开发期免审批

  prod:
    # 留空 = 用顶层默认
```

激活方式（优先级从高到低）：
1. `--profile <name>` CLI 标志
2. `--dev` 是 `--profile dev` 的别名
3. `UB_PROFILE` 环境变量
4. 未指定则用顶层配置

加载流程：
```
内置默认 → 全局 config.yaml → 工作目录 .ub/config.yaml → profile 覆盖 → CLI flag 覆盖
```

### 12.3 ub doctor

```text
$ ub doctor
╭─ providers ──────────────────────────────────────────────╮
│ ✓ anthropic        api.anthropic.com           reachable │
│ ✗ openai           api.openai.com              NO_API_KEY│
│ ✓ vllm-local       localhost:8000/v1           reachable │
│   └─ models: Qwen2.5-Coder-7B, Llama-3.1-8B              │
│   └─ tool-capable: Qwen2.5-Coder-7B (per ModelInfo)      │
╰──────────────────────────────────────────────────────────╯
╭─ external commands ──────────────────────────────────────╮
│ ✓ rg            14.1.0                                   │
│ ✓ gopls         v0.16.2                                  │
│ ✗ typescript-language-server                  NOT_FOUND  │
╰──────────────────────────────────────────────────────────╯
╭─ mcp servers ────────────────────────────────────────────╮
│ ✓ filesystem    stdio  started in 120ms                  │
╰──────────────────────────────────────────────────────────╯

Suggested dev profile (use --suggest to print full snippet):
  profiles.dev.default_model = vllm-local/Qwen2.5-Coder-7B-Instruct
```

实现要点：
- provider 探测：发 GET `${base_url}/models`（OpenAI 兼容协议都支持）；Anthropic 走轻量 messages 调用
- 外部命令：`exec.LookPath`
- MCP server 启动连通性检查：**I-13 不实现**（MCP client 在 I-29 才落地）；I-30 之后补回这一节
- 输出用 `lipgloss` 着色，但保留 `--plain` 选项给 CI

### 12.4 离线开发流：典型循环

> 想加一个新 tool / 改一段 prompt 模板：

1. 写 Go 代码
2. 配 fake provider 跑单测：`go test ./internal/app/ub/agent/...`
3. 跑 `ub run --dev -p "用新 tool 做某事"` 看真实模型怎么调
4. 满意了 → `UB_VCR=record go test ./internal/integration/...` 录一个 cassette
5. 平时 CI / 本地：`go test ./...`（默认走 cassette replay）

整个循环里 **零 token 消耗**（除步骤 3 用本地模型 + 步骤 4 一次性录制）。

## 13. 开发里程碑

按 35 个迭代组织，权威来源是 [`roadmap.md`](./roadmap.md)。这里只给版本与 Sprint 的对应关系（与 requirements §6 对齐）：

| 版本 | Sprint | 迭代 | 关键交付 |
|---|---|---|---|
| V0 脚手架 | Sprint 0 | I-01 ~ I-04 | 仓库、CLI、配置、SQLite、日志 |
| V1 MVP（Sprint 1） | Sprint 1 | I-05 ~ I-14 | 4 个 provider + fake、rollout 持久化、vcr、`ub doctor`、profile 支持 |
| V1 MVP（Sprint 2） | Sprint 2 | I-15 ~ I-21 | tool 体系、权限审批、agent loop with tools |
| V1 MVP（Sprint 3） | Sprint 3 | I-22 ~ I-26 | Bubble Tea TUI、diff 弹窗、slash 命令 |
| V1 MVP（Sprint 4） | Sprint 4 | I-27 ~ I-28 | 自动 summary、token 估算 |
| V1 MVP（Sprint 5） | Sprint 5 | I-29 ~ I-32 | MCP（stdio/http/sse）+ LSP（gopls） |
| V1.1 收尾 | Sprint 6 | I-33 ~ I-35 | session resume、`ub rollout show`、v0.1.0 release |
| V2 深化 | — | 未排 | 客户端/服务端拆分（HTTP API）、`/config reload` 热加载、loop detection、并行 tool call、更多 provider、skills/hooks |

## 14. 待办与开放问题

- [ ] TUI 运行指示器（footer spinner + elapsed），细节见 [`tui-animation.md`](./tui-animation.md)
- [ ] LSP 集成深度：V1 只做 diagnostics + references；rename / code action 留 V2
- [ ] Token 估算用 tiktoken-go？还是各 provider 自家 SDK 返回的 usage？决策：估算用 `tiktoken-go` 估个大概，准确数靠响应里的 usage 字段后置校正
- [ ] Windows 支持深度（bash 工具走 PowerShell？job 工具进程组语义不同）。先 Linux/macOS，Windows V2
- [ ] **已决** 配置语言：YAML 主体，JSON Schema 只用于校验，不再支持 JSON 配置文件
- [ ] **已决** 配置热加载（`/config reload`）：V2，不进 V1
- [ ] **已决** Rollout 耐久性：WAL + `synchronous=NORMAL`，不逐条 fsync
