# ub — 设计选型文档

> 状态：当前实现对齐（2026-07-09）。本文件与 `requirements.md`、`roadmap-v2.md` 共同描述当前产品边界。

## 1. 总体架构

```
                ┌────────────────────────────────────────────┐
                │                TUI Layer                    │
                │  Bubble Tea models: chat / diff / modal     │
                └───────────────┬────────────────────────────┘
                                │ events / commands (channel)
                ┌───────────────▼────────────────────────────┐
                │                App / Coordinator            │
                │  事件流分发、session 调度、eventbus/API（预留） │
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
│   ├── agent/                         # Agent/Factory、stream、tool runner、context、activity、summary
│   ├── command/                       # cobra 子命令：run / rollout / config / sessions
│   ├── tui/                           # Bubble Tea models 与 view
│   │   ├── theme/                     # lipgloss 主题
│   │   ├── diffview/                  # unified diff 渲染
│   │   ├── slash/                     # / 命令处理
│   │   └── permission/                # 权限审批弹窗
│   ├── config/                        # YAML 加载、profile 覆盖、schema
│   ├── message/                       # provider-neutral message model
│   ├── mode/                          # execution mode、mode gate、mode switch events
│   ├── reasoning/                     # reasoning effort 枚举与校验
│   ├── provider/                      # Provider 接口、Caps、Request、Stream、Event
│   │   ├── anthropic/                 # 包 anthropic-sdk-go
│   │   ├── openai/                    # 包 openai-go
│   │   ├── compat/                    # OpenAI 兼容（DeepSeek / Together / vLLM / Ollama /v1 …）
│   │   └── fake/                      # 单测用：脚本驱动，无 IO
│   ├── tokenizer/                     # token 估算
│   ├── context/                       # context window 解析
│   ├── modelinfo/                     # model capability 展示与合并
│   ├── vcr/                           # LLM 请求录制 / 回放
│   ├── permission/                    # 风险判定、always-rules、UI 回调
│   ├── approval/                      # auto 模式下的命令审批 agent
│   ├── hook/                          # hook runner
│   ├── logx/                          # slog 初始化与 rotation
│   ├── maintenance/                   # 启动期清理任务
│   ├── store/                         # SQLite session 持久化
│   ├── rollout/                       # rollout event 写入/读取
│   ├── workspace/                     # 本地文件状态
│   │   ├── paths/                     # XDG 与项目路径解析
│   │   ├── memory/                    # workspace memory
│   │   ├── filehistory/               # 文件快照与 rewind 支撑
│   │   └── tooloutput/                # 大工具输出落盘/摘要
│   ├── lsp/                           # LSP client
│   ├── mcp/                           # MCP client（stdio/http/sse）
│   └── tool/
│       ├── tool.go                    # Tool 接口、Risk、Registry
│       ├── fs/                        # read / write / edit / multiedit / apply_patch / ls / glob / tool_result
│       ├── plan/                      # plan_write / plan_update / plan_update_step
│       ├── todo/                      # todo_write / todo_update
│       ├── memory/                    # remember / recall
│       ├── search/                    # grep / glob
│       ├── shell/                     # bash
│       ├── job/                       # job_run / job_output / job_kill
│       ├── task/                      # task 子 agent adapter
│       ├── goal/                      # create_goal / get_goal / update_goal
│       ├── web/                       # web_search / web_fetch
│       └── mcp/                       # MCP tool adapter
├── docs/
├── .references/                       # 不入 git
└── go.mod
```

`internal/` 之外不暴露 API。未来若要拆 client/server，把 `internal/command` 的协调层抽到独立 server package，TUI 通过 HTTP/Unix socket 接入。

## 3. Agent Loop 设计

```go
// internal/agent/agent.go
type Agent struct {
    provider provider.Provider
    tools    *tool.Registry
    rollout  rollout.Writer
    model    string
    mode     execution.Mode
    modeFunc func() execution.Mode
    // 其余字段由 Options 注入: permission、events、summary provider、
    // context config、prompt config、hooks、memory、file history 等。
}

type Request struct {
    SessionID      string
    Turn           int
    History        []message.Message
    ContextHistory []message.Message
    Prompt         string
}

func (a *Agent) Run(ctx context.Context, req Request) (Result, error) {
    userMsg := message.Text(message.RoleUser, req.Prompt)
    transcriptMessages := append(clone(req.History), userMsg)
    contextMessages := append(effectiveContextHistory(req), userMsg)
    a.append(ctx, req.SessionID, rollout.UserMessage(req.SessionID, req.Turn, userMsg))

    turn := 0
    for {
        if maxTurns > 0 && turn >= maxTurns {
            // TUI 可通过 LimitAsker 给一次额外预算；无批准时进入 no-tools 收尾。
            return a.finalizeWithoutTools(ctx, req.SessionID, req.Turn, contextMessages, transcriptMessages, "tool loop reached max_turns")
        }

        // 1. 按当前 mode 生成工具 schema，并准备上下文（含 runtime context / summary）
        tools, err := a.toolDefinitions(a.currentMode())
        if err != nil { return Result{}, err }
        prepared, err := a.prepareMessages(ctx, req.SessionID, req.Turn, contextMessages, tools)
        if err != nil { return Result{}, err }

        // 2. 调 LLM（流式）
        stream, err := a.provider.Chat(ctx, provider.Request{
            Model: a.model,
            Messages: prepared.requestMessages,
            Tools: tools,
            Reasoning: a.reasoning,
        })
        if err != nil { return Result{}, err }

        // 3. 消费流：边收 delta 边推送 UI、收集 tool calls
        consumed, err := a.consumeStream(ctx, req.SessionID, req.Turn, stream, prepared.estimatedTokens)
        if err != nil { return Result{}, err }
        a.append(ctx, req.SessionID, rollout.AssistantMessage(req.SessionID, req.Turn, consumed.message))

        // 4. 若没有 tool call → 终止
        if len(consumed.toolCalls) == 0 { return Result{Text: consumed.text}, nil }

        // 5. 同一批 tool calls 并发执行，结果按 provider 返回顺序回灌
        results := make([]indexedResult, len(consumed.toolCalls))
        g, gctx := errgroup.WithContext(ctx)
        g.SetLimit(len(consumed.toolCalls))
        for i, call := range consumed.toolCalls {
            i, call := i, call
            g.Go(func() error {
                results[i] = indexedResult{call: call, result: a.runTool(gctx, req.SessionID, req.Turn, call)}
                return nil
            })
        }
        if err := g.Wait(); err != nil { return Result{}, err }
        toolResults := appendToolResultsInOrder(ctx, req.SessionID, req.Turn, results, &transcriptMessages, &contextMessages)
        turn++

        if loopDetector.Record(consumed.toolCalls, toolResults) {
            return a.finalizeWithoutTools(ctx, req.SessionID, req.Turn, contextMessages, transcriptMessages, "repeated tool loop detected")
        }
    }
}
```

要点：
- **最大 turn 数**：默认不按固定步数截断；只有配置 `max_turns > 0` 时才启用 hard guard。TUI 触顶时可通过 `LimitAsker` 追加一段预算，否则 agent 会发起一次禁用工具的收尾请求。
- **执行器生命周期**：`Agent` 是轻量执行器,不是长期状态容器。TUI/headless runner 可以按用户 turn 构造新的 `Agent`,并把 `session_id`、`turn`、`history`、`context_history`、rollout writer、permission manager、tool registry 等外置状态注入进去;不要用 agent 对象池承载对话状态。CLI runtime 使用进程内 provider cache 复用同一 provider config 对应的 provider/client,并用 `agent.Factory` 从共享 Options 模板创建新的主/子 Agent。
- **并行 tool call**：当前实现会并发执行同一批 provider tool calls，并按原始顺序把结果写回 history/rollout。写类工具仍通过 preview、permission、TOCTOU 校验和工具自身原子性降低冲突风险；跨工具写同一文件的高级调度仍属于后续深化。
- **loop detection**：内置基础重复检测；最近窗口内相同 tool-call/result 签名重复超过阈值时，agent 不再继续调用工具，而是发起一次禁用工具的收尾请求。更复杂的跨模式/跨会话策略放到 V2 深化。
- **取消**：`ctx` 由 TUI 的 Ctrl+C 触发 cancel，provider stream 中断
- **Prompt section registry**：provider 请求前缀按 `coding_agent -> runtime -> workspace_instructions -> git_snapshot -> execution_mode -> memory` 固定顺序建模。每个 section 同时携带内部 message 与 status/stability/source/truncation 元数据，provider messages 和 `ub prompt inspect` manifest 都从同一 section slice 投影，避免诊断视图与真实请求漂移。Agent 构造时捕获 startup sections，保证 Git snapshot 不会在每次 tool-loop 请求前刷新；execution mode 与 memory 在请求前动态生成。no-tool 路径复用同一 registry，但把 coding/workspace/git/mode 标为 `omitted`，只发送 no-tool runtime 和 memory。inspect 默认只输出元数据，`--show-content` 才显示可能含项目指令或 memory 的正文；命令不初始化 provider、tools、session 或 rollout
- **执行模式**：`mode` 从 CLI / profile / config 注入，影响 write/exec/network tool 的放行路径；mode 本身不作为 session 元数据持久化，resume 后使用本次运行的有效 mode。TUI slash 切换和模型发起 plan-mode 转换会更新当前进程状态，并以 `mode` activity 写入 rollout 供审计/恢复显示。
- **活动流**：Agent 对 provider reasoning、tool lifecycle、permission decision 产生结构化 activity 事件。reasoning 只透传 provider 返回的可展示摘要（Anthropic thinking、OpenAI-compatible `reasoning_content` / `reasoning` / `thinking` 等），不伪造隐藏思维链；TUI 将同一轮连续 reasoning delta 合并成一个可展开 thinking 区域，tool lifecycle 与 permission decision 合并到独立的 tool 区域，两个区域可分别折叠/展开。TUI 参考 opencode 的降噪思路：同一 tool call 用 `tool_use_id` 原地更新，默认只显示动作短标题，工具结果细节不展开到聊天区；`todo_write` / `todo_update` 例外,它们的 tool block 只保留审计摘要,TUI 从稳定 `## Todo` result 抽取并维护一个独立 Todo checklist 区块；同一 session 内 `todo_update` 原地刷新当前 checklist,新的 `todo_write` 视为替换清单并把 standalone checklist 移到当前工具事件附近；`task` 子 agent 没有独立 session,但其 start/done、tool lifecycle、permission 等 display-only activity 会镜像到父 session/turn,携带 `parent_tool_use_id`、`subagent_id`,并把 child tool id 命名空间化为 `subagent:<parent-task>:<child-tool>`;TUI 用 `subagent:` 前缀展示并在恢复 session 时重建这些 activity。子 agent reasoning delta 只走 live activity,不逐片持久化。所有内置工具和 `mcp__<server>__<tool>` 动态工具都必须有稳定可读的 running/done 标签，避免退回不明确的 `Working...`；展开 tool 区域后先展示每个工具的摘要，带详情的 write/edit/multiedit 工具项可通过活动焦点展开 colored unified diff；read/ls/glob/bash/task/MCP 等无文件 diff 的工具展开时按普通文本展示限幅后的 tool result 内容，不把 markdown 列表或普通 `+` / `-` 行当作 diff 着色；bash 会把 `<shell_metadata>` 转成普通 metadata 行；streaming partial output 在最终 done/failed 到达时不被空详情或仅 metadata 的详情覆盖；activity 层再次限幅详情时必须显示 `activity detail truncated` 提示，并在存在 tool result truncation footer 时保留 `full_output_path`；展开详情被当前 TUI 视窗裁剪但数据本身未限幅时，消息区底部显示 `[tool detail clipped: ...]` 提示。恢复 session 时，TUI 从 rollout `tool_result` payload 重建相同的摘要和详情，保留 files/truncation metadata，而不是只从 message history 的纯文本 tool_result 回放。
- **TUI 消息队列与回合内引导**：同一 session 内 Agent turn 仍保持串行。运行中用户的输入按发送方式分流，二者互斥：
  - `Tab` = **排队下一回合**：写入本地 FIFO 队列，不并发调用 Agent；当前 stream 正常关闭后自动取队首启动下一轮。排队消息在真正启动前不写入 rollout，避免被中断或编辑后的草稿污染历史。运行中上下方向键优先进入队列编辑，再退回普通历史输入浏览。
  - `Enter` = **回合内引导（inject）**：把文本作为补充 prompt 注入当前正在运行的 Agent loop，不另起回合。语义是「用户之前没说清楚、模型已在跑，现在补充内容」。inject 消息与初始 prompt **共用同一个 turn 编号**，是「turn N 这个回合中途插入的补充 prompt」，而非新回合；它先经 TUI 的 inject channel 投递给 Agent，Agent 在 tool-loop 迭代间（下一批 tool 结果之后、下一次 provider 调用之前）drain 出来，作为一条 user message 追加进 transcript 和 context，并以 `user_message` event 写入 rollout（turn = 当前回合）。rollout 按 `turn ASC, time ASC, rowid ASC` 排序，inject 靠 time/rowid 自然卡在正确位置，无需额外排序键。
  - inject 的落盘由 Agent 侧统一负责（只有 Agent 写 rollout，TUI 不越界直接写）。`drainInjected` 在两条路径上调用以保证不丢消息：(1) tool-loop 每次迭代执行完 tool calls 之后正常 drain，让模型在下一批 tool 结果之后、下一次 provider 调用之前读到引导；(2) `Run` 的所有出口（成功结束、error、maxTurns 终结）收尾时用 defer 兜底 drain 一次，把模型未来得及消费的残留 inject 落盘。成功路径下兜底 drain 的消息也并入 `Result.Messages`/`ContextMessages`，使 runner 的 in-memory history 与 rollout 一致；error 路径下（`Result` 为零值）只落盘不并入返回值，runner 不更新 in-memory history，待下次 `readChatHistory`（resume/rewind）重建一致状态。异常路径（含 context 取消）用 `context.Background()` 兜底写盘，确保已表达的用户意图不被静默丢弃。
  - rewind 对 inject 的处理：inject 共用 turn 后，同一 turn 在 rollout 里可能有两条 `user_message`（初始 prompt + inject）。`/rewind` 的 target 列表**按 turn 去重，每个 turn 只取第一条 user message 作为 target**；`DeleteFromTurn(N)` 本就删除该 turn 及之后所有 events（含 inject），inject 作为回合一部分一起被删，rewind 语义不变。
  - inject 不进 prompt history（不污染 `↑` 历史），channel 满时向用户 toast 提示而非静默丢弃。
  - `/btw [question]` 是队列与引导的共同例外：TUI 立即启动独立旁路 provider 请求，不取消当前 Agent turn，也不把问题放入主队列、不作为 inject。
- **TUI 启动覆盖**：直接运行 `ub` 打开 TUI 时支持 `--provider <name>` 与 `--model <id>`，走与 `ub chat` 相同的 provider/model 选择规则，只影响本次启动，不写回配置。
- **TUI provider 切换**：`/provider [provider] [model]` 在当前 TUI session 内切换后续主对话 provider；无参数时展示 provider picker，显式切换后刷新 model/effort 候选与状态栏。切换只写回当前 session 元数据，不写回配置；不指定 model 时优先保留目标 provider 可用的当前 model。
- **TUI session 恢复**：`ub --resume` 不再静默选择最近 session，而是在启动后打开当前 workspace 的历史 session picker；`ub --resume=<id>` / `ub --resume <id>` 仍在进入 TUI 前直接恢复指定 session。TUI 内 `/resume` 对齐 CLI resume 语义：无参数打开 session picker，带 session id 时直接恢复；`/sessions` 继续负责 session picker、直接切换和 `search <query>` 历史事件搜索。恢复时同时还原 session 元数据中的 provider 与 model；旧 session 若缺少 provider，会先按配置/远端模型列表尽力推断。
- **TUI rewind**：`/rewind` 从当前 session 的 rollout events 中列出历史 `user_message`，打开可筛选 picker；target 列表按 turn 去重（同一 turn 的初始 prompt 与回合内 inject 只列一条），选中某条后删除该 turn 及之后的 events（inject 作为回合一部分一并删除），重建 runner history、TUI transcript、下一 turn 和 context 状态，并把该 turn 的 user message 放回输入框。`/rewind <turn>` 可直接定位目标 turn。文件回退采用 Claude-style checkpoint：Agent 在每个 user turn 开始前写入 `file_history_snapshot` event，并把已跟踪文件的旧内容备份到 state root；`write` / `edit` / `multiedit` 和可安全解析为字面路径的 `bash` 删除（`rm` / `git rm`）会在真正执行前补齐当前 turn 的文件旧状态。picker 根据当前 workspace 与目标 checkpoint 做 dry-run：默认只回退对话并保留 workspace 文件，也可选择同时把已跟踪文件恢复到目标 user message 之前的状态。变量、通配符、命令内 `cd` 等不可靠 shell 路径不会进入文件历史；checkpoint 中没有可靠旧状态的文件会跳过并在 TUI 提示。
- **TUI 本地输入增强**：首个非空字符为 `!` 的输入绕过 Agent，输入区显示 shell 模式提示，直接复用本地 `bash` 工具执行并只在当前 TUI 以本地输出展示结果，不写入 rollout/history、不走权限审批、也不渲染为模型 tool 调用；`/btw [question]` 切换到独立的内存 BTW 视图，带问题时复用当前 provider/model/reasoning 与无工具 runtime context，追加当前 session text-only history、旁路系统提示和 BTW 视图内已完成 Q/A 后发起一次 `Tools=nil` 的 provider 请求，流式答案只写入 BTW 视图内存并复用普通助手消息的 Markdown renderer，任何 provider tool call 或伪工具调用标记都按错误处理；BTW 视图内输入文字并回车会继续追问，只把视图内 Q/A 作为临时 side history，不写入主 rollout/history；BTW 长输出使用独立滚动状态，不滚动主聊天区；底部状态行显示 BTW 视图、模型、状态（`answering` / `idle`）和退出提示，不展示主对话的 context/cwd/help 状态栏；`Esc` 退出 BTW 视图并清空该视图的临时 Q/A、草稿和滚动位置；普通输入中的 `@prefix` 触发 workspace 文件候选，选择后插入 `@relative/path` 文本引用，不自动读取文件内容；输入框为多行 textarea，`Enter` 发送、`Ctrl+J` 换行（所有终端通用），终端支持 Kitty 键盘协议时 `Shift+Enter` 也能换行（通过 SSH/tmux/老终端时不可用），按内容自动增高（上限约为终端高度 1/3，超出内部滚动），`@` 文件提及在光标所在行内匹配插入；输入组件开启 virtual cursor，由 textarea View 内嵌渲染反向光标块（`tea.View.Cursor` 为 nil、隐藏真实终端光标），保证光标在 CJK 宽度与软换行下始终与文本对齐、IME 预编辑绘制在当前输入行。

## 4. Tool 系统

```go
// internal/tool/tool.go
type Tool interface {
    Name() string
    Description() string
    Schema() *jsonschema.Schema
    Risk() Risk                              // safe / write / exec / network
    Execute(ctx context.Context, args json.RawMessage) (Result, error)
}

// 可选接口：写类工具实现它，dispatcher 会在 Execute 前调一次 Preview，
// 把结果交给 permission UI 渲染（diff / 摘要），用户确认后再 Execute。
// 这样 model 不需要感知 dry_run，调用仍是单步。
type PreviewableTool interface {
    Tool
    Preview(ctx context.Context, args json.RawMessage) (Preview, error)
}

// 可选接口：长时间运行或可增量输出的工具实现它，dispatcher 会把
// StreamEvent 转成 EventToolPartialOutput 推给 TUI；最终仍返回一个
// 普通 Result 给模型。
type StreamingTool interface {
    Tool
    ExecuteStream(ctx context.Context, args json.RawMessage, events chan<- StreamEvent) (Result, error)
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
    Content        string                    // 文本结果（回给模型的 tool_result）
    IsError        bool
    Files          []FileChange              // 执行后的实际改动摘要（可与 Preview 不完全一致）
    FullContent    string                    // 可选完整输出，进入 tooloutput 限幅/落盘
    Truncated      bool
    OriginalBytes  int
    FullOutputPath string
    Metadata       map[string]string         // query/url/provider/parser 等审计字段，不放 secret
}

type FileChange struct {
    Path        string
    Kind        string                       // "create" / "modify" / "delete"
    UnifiedDiff string                       // 可选；写类工具 Execute 会带上实际写盘 diff，供 TUI 展开详情
}
```

**风险等级**：
- `safe`：read / ls / grep / glob / ask / enter_plan_mode / exit_plan_mode / diagnostics / references / hover / completion / document_symbols / rename / code_action / tool_result / plan_write / plan_update / plan_update_step / todo_write / todo_update / create_goal / get_goal / update_goal / remember / recall / task
- `write`：write / edit / multiedit / apply_patch
- `exec`：bash / job_run / job_kill
- `network`：web_search / web_fetch

`ask`:让模型在确有用户偏好分叉时发起结构化问题。schema 为 `questions[]`(header、question、options、multi_select),TUI 以分步向导逐题渲染(一次一题,Enter 确认当前题并前进,末题 Enter 整体提交;←/→或 Tab 在题间前后切换),每题选项列表末尾追加一个虚拟 "Other" 项,选中后进入行内自由文本输入作为该题答案;question/option 文本超宽时换行完整显示而非截断。选择摘要回灌为 tool result 并写入 transcript(`ask answered: header: label` 或 `header: <自定义文本>`);headless `ub run` 无交互 asker 时不阻塞,而是返回让模型自行判断并说明假设的 tool result。它是 `RiskSafe`,不走 permission approval,plan 模式也可用;子 agent 默认不继承 asker。

`enter_plan_mode` / `exit_plan_mode`:模型发起的 plan-mode 状态切换。`enter_plan_mode(reason?)` 只在 `work` 模式广告,由 TUI 直接切到 `plan` 并记住内存态 `pre_plan_mode`,不在入口弹确认框;`auto` / `full-access` 默认不广告。`exit_plan_mode(plan_id, summary?)` 只在 `plan` 模式广告,要求带 `plan_write` / `plan_update` 返回的 `plan_id`;TUI 展示该精确 artifact 后才请求用户批准,缺失、无效或不存在的 artifact 会直接返回明确 tool error,不会弹批准框。批准退出时恢复 `pre_plan_mode`(缺失则回到启动有效 mode 或 `work`),拒绝则留在 plan 模式让模型修订同一个 artifact。两者都是 `RiskSafe`,不走 permission approval,但 TUI 会把入口自动切换和出口用户批准/拒绝写成 mode activity 并进入 rollout 审计。

`plan_write` / `plan_update` / `plan_update_step`:把 plan-then-execute 工作流落到磁盘。plan 模式产出 `$XDG_STATE_HOME/ub/plans/<project-key>/<id>.md`(标题、metadata、`## Steps` 任务列表、`## Notes`、`## Log`),TUI 在 `plan_write` / `plan_update` 完成摘要中直接展示 `plan_id`。用户纠正已有计划时用 `plan_update` 原地更新同一个 artifact;也可在 TUI 中通过 `/plans` 打开当前 workspace 的 plan picker,或通过 `/plan-edit <plan-id>` / `/plans <plan-id>` 用 `$VISUAL` / `$EDITOR` 直接打开该 markdown review/edit。work/auto 模式按这个 artifact 推进并 `plan_update_step` 标记每一步。`plan_write` / `plan_update` 只在 plan 模式暴露和执行;`plan_update_step` 只在 work/auto 模式用于执行进度。plan 模式只向 provider 广告 `read` / `ls` / `glob` / `grep` / `ask` / `plan_write` / `plan_update` / `exit_plan_mode` / `get_goal`,误调用其它工具仍由 mode gate 拦截。这些 plan artifact 工具都是 `RiskSafe`(写的是 ub 用户 state artifact 目录,不是用户代码)。

`todo_write` / `todo_update`:维护当前 session 的短生命周期执行清单,不复用 plan markdown checkbox。`todo_write` 创建或替换当前清单,`todo_update` 通过 `id` 或 1-based `item_index` 更新单项状态。状态为 `pending` / `in_progress` / `completed` / `skipped` / `failed`,并约束同一清单最多一个 `in_progress`。todo state 存在 `$XDG_STATE_HOME/ub/todos/<session-id>.json`,tool result 同时输出稳定的 `## Todo` 文本;TUI 将该文本抽取成独立 Todo checklist,rollout show 和 resume 也能恢复同一份执行视图。todo 工具只在 work/auto/full-access 执行阶段暴露;plan 模式不广告也不执行。

`create_goal` / `get_goal` / `update_goal`:维护当前 session 的长任务目标状态。goal state 存在 `$XDG_STATE_HOME/ub/goals/<session-id>.json`,包括 objective、status、token/turn budget、已用 token/turn、阻塞原因和时间戳。`create_goal` 创建 active goal,已有非终止 goal 时拒绝覆盖;`get_goal` 返回当前状态;`update_goal` 允许模型在完成、连续阻塞或暂停时更新状态。TUI `/goal [objective|clear]` 可创建/查看/清除当前 session goal;headless `ub goal -p ...` 会预创建 goal 并自动续跑 agent turn,直到 goal complete、blocked、paused 或预算耗尽。goal 不替代 plan/todo:goal 是跨 turn objective 和停止条件,plan 是持久方案 artifact,todo 是当前执行清单。

LSP 工具家族(全部 `RiskSafe`):`diagnostics` / `references` 之外,新增 `hover`、`completion`、`document_symbols`、`rename`、`code_action`。其中 `rename` 与 `code_action` **只返回 LSP 的建议**,不直接落盘 —— rename 输出"按文件路径排序的边界列表",model 拿到后用 `apply_patch` 或 `multiedit` 自行应用,从而走 ub 的 preview/permission 协议;`code_action` 只列可用 action 的 `title (kind)[ — has_edit]`,不执行任何 action

`tool_result(tool_use_id, offset?, limit?)`：从 `<state-root>/tool_outputs/<sessionID>/<toolUseID>.txt` 读回曾被 `tooloutput.LimitResult` 截断/落盘的完整工具输出。sessionID 由 agent 调用前通过 `tool.WithSessionID(ctx)` 注入到 context；工具自身不接受任意路径,只能读 spillover 目录,跨 session 不可见

`web_search(query, recency?, domains?, limit?)` / `web_fetch(url, max_chars?)`：内置联网检索工具,`tools.web.enabled` 默认开启,plan 模式不广告也不执行。两者都是 `RiskNetwork`,默认走与 exec 同级的人类/approval-agent 审批路径,permission rule 使用 `WebSearch(domain:golang.org)` 或 `WebFetch(docs.python.org:*)` 这样的目标。`web_search` 默认使用零配置、无需 API key 的 DuckDuckGo HTML provider,也支持 Brave/Tavily/SerpAPI/SearXNG provider,输出 provider-neutral 的 title/url/summary/date;商业 provider 缺 key 或 SearXNG 缺 base_url 时返回清晰 tool error。`web_fetch` 仅抓 HTTP(S),默认拒绝 file/local/private 网段,检查 robots.txt,限制 timeout/redirect/source bytes,对 HTML/PDF/text 做最小正文提取。结果进入统一 `context.tool_results` 限幅和 spillover,rollout `tool_result.metadata` 记录 query/url/final_url/provider/parser/content_type/source_bytes 等审计字段,不记录 API key。默认 `user_agent` 使用浏览器兼容的 crawler 标识(`Mozilla/5.0 (compatible; ub-web/1.0)`),用户可通过 `tools.web.user_agent` 覆盖。

**Registry**：本地工具静态注册，MCP 工具运行时注册。同名冲突时 MCP 走 `mcp__<server>__<tool>` 前缀（Anthropic 规范）。

**Streaming tools**：工具可选择实现 `StreamingTool` 接口(继承 `Tool` 之上多一个 `ExecuteStream(ctx, args, events chan<- StreamEvent)`)。agent runtime 检测到该接口时,在新 goroutine 跑 `ExecuteStream`,把每条 `StreamEvent{Kind,Data}` 转成 `EventToolPartialOutput` 推到 EventSink,让 TUI 在工具未结束前看到滚动预览。`bash` 已接入 stdout/stderr streaming;`job_output` 在 `follow=true` 时先推当前 ring buffer 快照,再追踪新增 stdout/stderr,直到 job 退出、`timeout_ms` 到期或请求取消。其他工具仍同步 `Execute`,不发 partial 事件。chunk 在 emit 前截到 4KB。

**两阶段调用流程**（仅 PreviewableTool）：
```
1. agent 解析 tool_call
2. tool := registry.Get(name)
3. mode gate:
     - plan + write risk: 拒绝，回灌 ToolResult{IsError, Content="plan mode is read-only"}
     - plan + exec/network risk: 拒绝，回灌对应只读错误
     - work / auto / full-access + write/exec/network risk: 继续
4. if pt, ok := tool.(PreviewableTool); ok:
       preview = pt.Preview(ctx, args)
5. exec/network risk:
     - allow-rule match: 直接 Allow（黑名单除外）
     - work / plan: permission.AskHuman(call, preview)
     - auto: permission.AskApprovalAgent(call)；拒绝 / 不确定 / 错误时回退 AskHuman
6. if decision != Allow: 回灌 ToolResult{IsError, Content="denied"} 给模型
7. else: result = tool.Execute(ctx, args)
```

**关键 tool 实现要点**：
- `edit` / `write`：实现 `PreviewableTool`。Preview 读现盘 + 在内存里应用 patch + 用 `go-udiff` 算 unified diff；Execute 实际写盘，并在 `FileChange.UnifiedDiff` 中返回实际变更。`edit` 默认用 `old` / `new` 精确子串替换，`old` 必须逐字节匹配；当模型难以从带行号的 `read` 输出复原 tab、空格或换行时，可用 `start_line` / `end_line` 替换完整行，仍通过同一 preview / permission / TOCTOU 路径。TUI 默认只展示摘要，按 `Ctrl+O` 展开最近的 tool 区域后先展示工具摘要，再按一次展开最近工具项的着色文件级详情；也可用 `Ctrl+N` / `Ctrl+P` 移动活动焦点并用 `Enter` / `Space` 操作任意活动块或工具项；TUI 默认不启用鼠标追踪，保留终端原生拖拽选择复制。`multiedit`（一次调用跨文件多处编辑）共用 `applyEdit` 与 `udiff`，在内存中按数组顺序对同 path 串行累加，先对所有目标做 TOCTOU 二次读校验再批量写盘，任一步失败即不写盘（写过的文件回滚到 before 快照），从而对调用方提供 all-or-nothing 语义
- `apply_patch`：实现 `PreviewableTool`。输入是 `*** Begin Patch` 信封，支持 Add / Update / Delete 与 Update 后的 Move；Update hunk 的上下文和删除行必须在当前内存文件中唯一、逐行精确匹配，歧义或失配时拒绝而不猜测。Preview 的已验证计划按 `tool_use_id` 绑定到 Execute，Execute 会用该 before 快照拒绝审批期间的外部改动。全部补丁 I/O 通过受 workspace root 约束的文件句柄，拒绝经 symlink 访问 workspace 外文件；提交采用同目录临时文件、`Sync`、显式 mode 和 `Rename`，避免截断原文件并保留 Move 的 mode。中途失败会按 before 快照恢复已变更文件。成功后对仍存在的最终文件发 LSP `didChange`，并把同一已验证解析结果交给 file-history checkpoint。
- `bash`：用 `os/exec` 拉子进程，stdout/stderr 流式回传；超时默认 120s；不实现 Preview（命令是黑盒）
- `job_run`：返回 `job_id`，进程交给后台 goroutine 管理；`job_output(job_id, tail?)` 返回当前 stdout/stderr 快照；`job_output(job_id, follow=true, timeout_ms?)` 通过 `StreamingTool` 推送当前快照和新增输出,最终仍返回同一快照格式；`job_kill` SIGTERM/SIGKILL
- tool 参数解析对模型常见 JSON 标量抖动做窄容错：整数参数接受整数或整数字符串，布尔参数接受布尔值或 `"true"` / `"false"`，但 JSON Schema 仍对外声明真实 integer/boolean 类型
- `references` 优先支持 `symbol` + 可选 `path` 的符号名查询，由本地搜索定位候选位置后再调用 LSP；兼容 `file` + `line` + `col` 的位置查询

## 5. Provider 抽象

```go
// internal/provider/provider.go
type Provider interface {
    Name() string
    Caps() Caps
    Chat(ctx context.Context, req Request) (Stream, error)
}

// 可选：provider 能按模型返回不同能力时实现。
func CapsForModel(p Provider, model string) Caps

type Caps struct {
    SupportsTools       bool
    SupportsStreaming   bool
    SupportsPromptCache bool
    MaxContextTokens    int
    SupportsVision      bool
}

type Request struct {
    Model     string
    Messages  []message.Message
    Tools     []ToolDefinition
    Reasoning *reasoning.Config
}

type Stream interface {
    Next(ctx context.Context) (Event, error)  // text delta / reasoning delta / tool call / usage / done
    Close() error
}
```

**双层抽象**（借鉴 codex-rs 的 `model-provider` + `model-provider-info`）：
- `Provider` 是行为接口
- `internal/modelinfo.Info` 是模型元信息（reasoning 能力、effort、context window 等），由配置文件 + 内置默认表合并
- `Provider.Caps()` 描述 provider 默认能力；`CapsForModel(p, model)` 在 provider 支持时叠加模型级能力
- reasoning 能力按 `用户配置覆盖 > 内置 modelinfo 表 > 保守未知模型` 解析；未知模型默认不发送 reasoning 参数

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
- max context 统一由 `internal/context.Resolver` 解析并附带 source/confidence。显式 `providers.<name>.models.<model>.max_context_tokens` 始终优先；未配置时使用模型元信息/provider `CapsForModel`，并允许真实 usage 抬高被事实否定的静态值、带数值 overflow 修正兼容端点的错误默认值
- 主 provider usage 与可识别的 context overflow 会按 provider 名、清理敏感部分后的 base URL 和完整 model ID 写入 `$XDG_STATE_HOME/ub/context-windows/<key>.json`。每 key 文件以 `0600` 原子替换；缓存只含 token 观察，不含 prompt/消息/API key，损坏或不可写时退回静态候选而不阻断请求
- 请求前由 `internal/context` 的纯数据 planner 从 `ContextSnapshot` 产生 `ContextDecision`（`keep` / `prune` / `compact` / `compact-and-retry`，reason 为 threshold/manual/overflow/incomplete/mid_turn）。自动路径按 `estimated_input + context.reserve_output_tokens > max_context * context.trigger_ratio` 进入决策，先以同一 `tool_use_id` 的占位结果安全裁剪可证明被后续同输入 `read` / `grep` 覆盖或明确为空的旧结果；当前 turn、保留窗口、错误和会修改文件/验证的结果受保护。裁剪后按实际 request estimate 再次判断，仍超预算且有完整 turn 前缀才 summary。provider 返回可识别上下文超限时，Agent 回灌窗口观察并最多执行一次 `compact-and-retry`；TUI 的 `/compact` 复用 manual 决策。最近原文仍按 `context.keep_recent_turns` + token budget 与完整 user turn 边界保留，绝不孤立 tool_use/tool_result
- summary prompt 自身也按 summary 模型的 context window 做预算控制：待摘要 conversation 超过预算时，Agent 只按完整 user turn 边界打包分块，再把块摘要递归合并，避免主模型已超限后 summary 模型继续收到同样超限的历史；单个 user turn 自身超预算时不做 message/字符级切碎，直接返回明确错误
- tool result 在进入下一次 provider 请求和写入 rollout 前统一限幅：默认最多 12KiB/400 行模型可见内容；超限时完整输出写入 `$XDG_STATE_HOME/ub/tool_outputs/<session>/<tool_use>.txt`（否则 `~/.local/state/ub/...`），rollout 只保存 preview、`truncated`、`original_bytes`、`full_output_path`
- 每次实际 prune/summary/retry 都在 summary rollout payload 中保存最终 provider context 以及 action/reason、token before/after、cut boundary、裁剪/保护 ID、summary model、耗时和 retry；这些审计字段不保存 prompt 或完整工具输出。读取 rollout 历史时保留完整可见 transcript，维护事件不作为用户/助手消息渲染；同时为 provider 请求单独构造 context history，遇到该事件即使用保存的 context（summary + 保留原文或带占位结果的 prune-only context）替换请求上下文。prune-only checkpoint 仅用于恢复，不得从其保存的 provider context 派生 `ub sessions search` 正文。旧 `Summary` 事件没有 messages 时仍退回单条 system summary message

**重要的内部消息表示**：
不要直接复用 anthropic / openai 的请求类型。在 `internal/message/` 自定义中性 `Message` 结构（`Role`、`Content[]`；content block 包含 text / reasoning / image / tool_use / tool_result），各 provider 各自转换。理由：避免被某家 SDK 锁定。

## 6. Rollout（事件日志）

```go
// internal/rollout/event.go
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

**清理与截断**：启动时做 best-effort 自动清理，默认最多每 24h 运行一次（记录在 `$XDG_STATE_HOME/ub/cleanup.json`，否则 `~/.local/state/ub/cleanup.json`）。默认删除 30 天未更新且不属于其 workspace 最近 20 个的 session；`events` 不做自动单 session 内局部裁剪，只随 `sessions` 的 `ON DELETE CASCADE` 删除，避免破坏历史恢复、summary 和 rollout replay。删除 session（显式 `sessions rm/delete`、`sessions clear`、全局 clear 或启动过期清理）会同时清理 session 关联的 state artifacts：`$XDG_STATE_HOME/ub/todos/<session-id>.json`、`$XDG_STATE_HOME/ub/goals/<session-id>.json`、`$XDG_STATE_HOME/ub/file-history/<session-id>/`、默认 tool-output spillover 目录 `$XDG_STATE_HOME/ub/tool_outputs/<session-id>/`，以及该 session 的 rollout `tool_result` 引用且未被其它 session 引用的 plan markdown 文件。唯一的单 session 局部截断入口是用户显式 `/rewind`：删除目标 turn 及之后 events 后立即重建内存 history 与 TUI 显示。tool-output spillover 文件按 `context.tool_results.spillover_max_age` 清理，失败只 warning。启动清理失败只写 warning，不阻断 CLI/TUI 主流程；默认不执行 SQLite `VACUUM`，避免启动时长时间阻塞。

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

tools:
  job:
    max_concurrent: 50
    retention: 8h
    cleanup_interval: 5m
  web:
    enabled: true
    provider: duckduckgo   # duckduckgo / brave / tavily / serpapi / searxng
    api_key: ${WEB_API_KEY}
    base_url: https://search.example.com
    user_agent: Mozilla/5.0 (compatible; ub-web/1.0)
    timeout: 15s
    max_fetch_bytes: 2097152
    allow_domains: []
    deny_domains: []
    allow_private_network: false

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
- `work`：允许 workspace 内文件读写；广告 `enter_plan_mode` 供模型在复杂实现前请求只读规划；`exec` 风险工具如果没有命中 allow-rule，走用户审批。
- `plan`：只读规划；只广告 `read` / `ls` / `glob` / `grep` / `ask` / `plan_write` / `plan_update` / `exit_plan_mode` / `get_goal`。`write` 与 `exec` 风险工具、sub-agent、memory、LSP/MCP 等其它工具在 dispatcher 层直接拒绝并把错误回灌给模型。
- `auto`：文件读写策略同 `work`；默认不广告 `enter_plan_mode`,避免打断连续执行语义；`exec` 风险工具先交给 approval agent 自动判断，若拒绝、不确定或异常，再回退到用户显式审批。
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
1. tool dispatcher 收到 call，先执行 mode gate：`plan` 模式只允许 read / search / ask / plan / exit_plan_mode 这类安全规划工具
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

**auto memory 调度(两条分支)**：auto memory 分显式和自动两条独立分支,不共享判定逻辑。

- **分支 1 — 显式记忆**:用户在主对话里明确要求"记住 X"时,主模型直接调用 `remember` 工具写入 memory；要求"忘记 X"时，先用 `recall` 确认项目 auto-memory 的精确文本/分类，再调用 `forget` 删除。`forget` 仅操作 machine-managed auto-memory，不修改 append-only 的全局手写指令。两种工具调用都走正常 tool runner，并以 `memory_write`（`action=created|merged|deleted`）审计；本轮调用任一种工具后，调度器都会跳过自动抽取，避免重复写入。
- **分支 2 — 自动抽取**:其余成功 turn 全部进入后台 `MemoryAutoScheduler`。调度器在前台只做硬门控:plan 模式、空 workspace、显式 memory 工具已处理的 turn、默认配置下包含 MCP / web / tool_search 等外部上下文工具的 turn 直接跳过;**不做任何关键字预过滤**,把"什么值得记"的判定完全下放给 small model + prompt。批处理、pending job、最小间隔和退避状态都按 session 隔离；同一 runner 切换 session 时会丢弃未处理的旧批次，绝不合并到新会话；已经运行的旧任务保留其原会话 rollout 到审计完成，后台事件携带 session 标识并由 TUI 过滤，绝不显示到新会话。其余 turn 按 `memory.auto.trigger`、累计 turn 数、累计可见消息数和最小间隔批量调度后台 small-model 抽取;正在抽取时只保留一个合并后的 pending job。`memory.auto.max_prompt_chars` 是 taxonomy 模板、已有 auto-memory 摘要和 turn 内容合计的硬字符上限；显式正值至少为 1024，不足以容纳完整 taxonomy 时使用紧凑等价模板。抽取 prompt 注入已有 auto memory 摘要(按优先级截断到最多 1000 字符),让 small model 自行判断 update vs create vs skip。只有成功完成且没有写入的抽取才会累计空结果；provider 或写入失败不触发退避。连续 3 次空结果时,调度器进入退避模式(有效阈值翻倍),直到下一次成功写入后重置。TUI 不等待抽取完成;headless `ub run` 在主答案输出后按 `memory.auto.drain_timeout` 做 best-effort drain。实际写入仍走 `memory.AppendWithOutcome` 和 `memory_write` rollout 事件。

**auto memory 抽取 prompt 设计**：借鉴 Claude Code 的 four-type taxonomy 思路,把"什么值得记"显式落到 prompt 里,而非靠 small model 自由发挥。prompt 用 `<types>` XML 块声明四类语义角色(user/feedback/project/reference),每类给出 `<when_to_save>` 和 `<examples>`,让 small model 在抽取时按语义匹配而非按关键词猜。ub 的 6 个 storage category(preference/project/pattern/decision/debug/general)是存储层概念,与语义角色不一一对应;prompt 显式给出角色→category 映射:user role → preference,feedback → preference(带 Why/How to apply),project → project/reference,reference → general。同时加入 Claude Code 的 H2 explicit-save gate 规则:排除清单即使用户显式要求保存也仍然适用——如果用户要求保存 PR list 或活动总结,引导 small model 反问"什么是 surprising 或 non-obvious 的部分",只保存那部分,而不是把活动日志当 memory。最后,把"代码模式/架构/文件路径/git history/调试 recipe/CLAUDE.md 已记录的内容/临时任务状态"列为显式排除项,因为这些都是可从代码或 git 推导的。

**黑名单**：硬编码的强制再确认正则（`rm\s+-rf\s+/`、`mkfs\.`、`dd\s+.*of=/dev/`）。即使任意 always-rule match 也再弹一次。

## 10. 测试策略

**单元测试**：
- `tool/*` 各工具：构造临时目录跑真实 IO
- `agent/`：用 fake provider（返回固定 stream）测 loop
- `context/`：token 估算 + 触发 summary 边界
- `permission/`：always-rules 匹配、黑名单优先级

**集成测试 + vcr**（`internal/vcr/`）：
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
| LSP | `gopls.dev/protocol` 或自家精简实现 | diagnostics / references / hover / completion / document_symbols / rename / code_action；rename/code_action 只返回建议 |
| MCP | 自家实现 client（参考 modelcontextprotocol 官方 schema） | Go 官方库不成熟时手写 |
| 测试 | `testing` + `testify/require` | |
| 录制 | 自家 vcr（内部包） | |

## 12. 开发与测试机制

针对学习/研究向项目，最大的摩擦是"想验证一个想法时不愿打真实 LLM"。提供三种互补路径：

### 12.1 fake provider（单元测试用）

- 包：`internal/provider/fake/`
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
│   └─ tool-capable: Qwen2.5-Coder-7B (per modelinfo)      │
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
2. 配 fake provider 跑单测：`go test ./internal/agent/...`
3. 跑 `ub run --dev -p "用新 tool 做某事"` 看真实模型怎么调
4. 满意了 → `UB_VCR=record go test ./internal/integration/...` 录一个 cassette
5. 平时 CI / 本地：`go test ./...`（默认走 cassette replay）

整个循环里 **零 token 消耗**（除步骤 3 用本地模型 + 步骤 4 一次性录制）。

### 12.5 Eval MVP（真实 Agent 行为评测）

`ub eval --task <name-or-path>` 加载 `docs/eval-tasks/*.yaml` 或显式 YAML 路径。task schema v1 包含首个 prompt、可选 follow-up prompts、fixture、timeout，以及文件/命令/rollout 断言。runner 不复制 Agent loop，而是启动当前 executable 的 `ub --mode full-access run`；多 turn task 从隔离 store 取得首轮 session ID，再通过隐藏的 `run --session` 复用同一 history/context。

每次运行创建临时 workspace，并把 `XDG_STATE_HOME`、`XDG_DATA_HOME` 指向该次临时根目录；全局 provider 配置仍按正常加载规则读取。fixture 复制和文件断言拒绝绝对路径、`..` 逃逸与 symlink。task 的命令断言按 argv 直接在临时 workspace 执行，因此只应运行受信 task。

完成后从隔离 rollout 汇总 turn、usage/cache token、tool result 序列和 summary 的 ContextDecision action/reason，再联合文件、验证命令、assistant 文本断言生成报告。默认文本输出；`--json` 输出单个稳定对象。模型运行失败与断言失败分别标记 `agent` / `assertion` failure category，并返回非零状态。MVP 只运行单个 task；批量任务、模型矩阵和统计报告留给 v0.6。

## 13. 开发里程碑

V1 的 35 个迭代来源 [`roadmap.md`](./roadmap.md) 已完成并作为历史存档；当前主动演进的权威来源是 [`roadmap-v2.md`](./roadmap-v2.md)。这里保留版本与 Sprint 的对应关系（与 requirements §6 对齐）：

| 版本 | Sprint | 迭代 | 关键交付 |
|---|---|---|---|
| V0 脚手架 | Sprint 0 | I-01 ~ I-04 | 仓库、CLI、配置、SQLite、日志 |
| V1 MVP（Sprint 1） | Sprint 1 | I-05 ~ I-14 | 4 个 provider + fake、rollout 持久化、vcr、`ub doctor`、profile 支持 |
| V1 MVP（Sprint 2） | Sprint 2 | I-15 ~ I-21 | tool 体系、权限审批、agent loop with tools |
| V1 MVP（Sprint 3） | Sprint 3 | I-22 ~ I-26 | Bubble Tea TUI、diff 弹窗、slash 命令 |
| V1 MVP（Sprint 4） | Sprint 4 | I-27 ~ I-28 | 自动 summary、token 估算 |
| V1 MVP（Sprint 5） | Sprint 5 | I-29 ~ I-32 | MCP（stdio/http/sse）+ LSP（gopls） |
| V1.1 收尾 | Sprint 6 | I-33 ~ I-35 | session resume、`ub rollout show`、v0.1.0 release |
| V2 深化 | — | `roadmap-v2.md` | 已落地 ask、task、memory、plan/todo、multiedit、tool streaming、扩展 LSP、tool_result、web 工具、模型发起 plan mode、full-access、goal mode、prompt section/inspect、上下文窗口 resolver、分阶段 ContextDecision/语义裁剪、最小 provider CachePlan 和 eval MVP；下一步基于 eval 数据扩展 prompt/context，并进入事件总线/tracing |

## 14. 待办与开放问题

- [ ] TUI 运行指示器（footer spinner + elapsed），细节见 [`tui-animation.md`](./tui-animation.md)
- [x] LSP 集成深度：diagnostics / references / hover / completion / document_symbols / rename / code_action 已接入；rename / code_action 仅返回建议，不直接落盘
- [ ] Token 估算用 tiktoken-go？还是各 provider 自家 SDK 返回的 usage？决策：估算用 `tiktoken-go` 估个大概，准确数靠响应里的 usage 字段后置校正
- [ ] Windows 支持深度（bash 工具走 PowerShell？job 工具进程组语义不同）。先 Linux/macOS，Windows V2
- [ ] **已决** 配置语言：YAML 主体，JSON Schema 只用于校验，不再支持 JSON 配置文件
- [ ] **已决** 配置热加载（`/config reload`）：V2，不进 V1
- [ ] **已决** Rollout 耐久性：WAL + `synchronous=NORMAL`，不逐条 fsync
