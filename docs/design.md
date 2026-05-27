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
├── internal/
│   ├── cli/                           # cobra 子命令：run / rollout / config / sessions
│   ├── app/                           # 协调层，事件总线，session 调度
│   ├── agent/                         # agent loop、prompt 模板、summary
│   ├── execution/                     # execution mode、mode gate、mode switch events
│   ├── provider/
│   │   ├── provider.go                # Provider 接口、ProviderCaps、ModelInfo
│   │   ├── anthropic/                 # 包 anthropic-sdk-go
│   │   ├── openai/                    # 包 openai-go
│   │   ├── compat/                    # OpenAI 兼容（DeepSeek / Together / vLLM / Ollama /v1 …）
│   │   └── fake/                      # 单测用：脚本驱动，无 IO
│   ├── tool/
│   │   ├── tool.go                    # Tool 接口、Risk、Registry
│   │   ├── fs/                        # read / write / edit / multiedit / ls / glob
│   │   ├── search/                    # grep / glob
│   │   ├── shell/                     # bash
│   │   ├── job/                       # job_run / job_output / job_kill
│   │   └── mcp/                       # MCP tool adapter
│   ├── mcp/                           # MCP client（stdio/http/sse）
│   ├── lsp/                           # LSP client + 工具桥接
│   ├── permission/                    # 风险判定、always-rules、UI 回调
│   ├── approval/                      # auto 模式下的命令审批 agent
│   ├── session/                       # session CRUD
│   ├── rollout/                       # 事件类型、append writer、reader
│   ├── store/                         # SQLite 封装（sqlc 或手写）
│   ├── config/                        # YAML 加载、profile 覆盖、schema
│   ├── context/                       # token 估算、压缩 / summary 策略
│   ├── diff/                          # unified / split diff 渲染数据
│   ├── tui/                           # Bubble Tea models 与 view
│   └── vcr/                           # LLM 请求录制 / 回放
├── docs/
├── schema/
│   └── config.schema.json
├── .references/                       # 不入 git
└── go.mod
```

`internal/` 之外不暴露 API。未来若要拆 client/server，把 `app/` 抽到独立 `pkg/server`，TUI 通过 HTTP/Unix socket 接入。

## 3. Agent Loop 设计

```go
// internal/agent/agent.go
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

    for turn := 0; turn < maxTurns; turn++ {
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
        for _, call := range result.ToolCalls {
            if err := a.runTool(ctx, call); err != nil {
                // tool 错误回灌给模型，让它处理
            }
        }
    }
    return ErrMaxTurns
}
```

要点：
- **最大 turn 数**：默认 25，防止无限循环（codex / Crush 通用做法）
- **并行 tool call**：V1 顺序执行，V2 再考虑并行（要保证写操作串行）
- **loop detection**：Crush 有 `loop_detection.go`（同一 tool call 重复 N 次抑制），V2 引入
- **取消**：`ctx` 由 TUI 的 Ctrl+C 触发 cancel，provider stream 中断
- **执行模式**：`mode` 从 session / CLI / profile 注入，影响 write/exec tool 的放行路径；模式切换写 `ModeSwitch` 事件
- **活动流**：Agent 对 provider reasoning、tool lifecycle、permission decision 产生结构化 activity 事件。reasoning 只透传 provider 返回的可展示摘要（Anthropic thinking、OpenAI-compatible `reasoning_content` / `reasoning` / `thinking` 等），不伪造隐藏思维链；TUI 将同一轮连续 reasoning delta 合并成一个可展开 thinking 区域，tool lifecycle 与 permission decision 合并到独立的 tool 区域，两个区域可分别折叠/展开。TUI 参考 opencode 的降噪思路：同一 tool call 用 `tool_use_id` 原地更新，默认只显示动作短标题，工具结果细节不展开到聊天区；展开 tool 区域后先展示每个工具的摘要，带详情的 write/edit 工具项可通过活动焦点展开 colored unified diff；工具 activity 只展示白名单摘要、截断长文本并遮蔽 secret。
- **TUI 消息队列**：同一 session 内 Agent turn 仍保持串行。运行中用户输入普通消息并回车时，TUI 只写入本地 FIFO 队列，不并发调用 Agent；当前 stream 正常关闭后自动取队首启动下一轮。排队消息在真正启动前不写入 rollout，避免被中断或编辑后的草稿污染历史；运行中上下方向键优先进入队列编辑，再退回普通历史输入浏览。
- **TUI 启动覆盖**：直接运行 `ub` 打开 TUI 时支持 `--provider <name>` 与 `--model <id>`，走与 `ub chat` 相同的 provider/model 选择规则，只影响本次启动，不写回配置。
- **TUI provider 切换**：`/provider [provider] [model]` 在当前 TUI session 内切换后续主对话 provider；无参数时展示 provider picker，显式切换后刷新 model/effort 候选与状态栏，不写回配置。
- **TUI session 恢复**：`ub --resume` 不再静默选择最近 session，而是在启动后打开当前 workspace 的历史 session picker；`ub --resume=<id>` / `ub --resume <id>` 仍在进入 TUI 前直接恢复指定 session。
- **TUI 本地输入增强**：首个非空字符为 `!` 的输入绕过 Agent，输入区显示 shell 模式提示，直接复用本地 `bash` 工具执行并只在当前 TUI 以本地输出展示结果，不写入 rollout/history、不走权限审批、也不渲染为模型 tool 调用；普通输入中的 `@prefix` 触发 workspace 文件候选，选择后插入 `@relative/path` 文本引用，不自动读取文件内容；输入组件关闭 virtual cursor，由每帧 `tea.View.Cursor` 暴露输入框真实光标，保证 IME 预编辑绘制在当前输入行。

## 4. Tool 系统

```go
// internal/tool/tool.go
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
- `safe`：read / ls / grep / glob / diagnostics / references
- `write`：write / edit / multiedit
- `exec`：bash / job_run / job_kill

**Registry**：本地工具静态注册，MCP 工具运行时注册。同名冲突时 MCP 走 `mcp__<server>__<tool>` 前缀（Anthropic 规范）。

**两阶段调用流程**（仅 PreviewableTool）：
```
1. agent 解析 tool_call
2. tool := registry.Get(name)
3. mode gate:
     - plan + write risk: 拒绝，回灌 ToolResult{IsError, Content="plan mode is read-only"}
     - work / auto + write risk: 继续
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
- `edit` / `write`：实现 `PreviewableTool`。Preview 读现盘 + 在内存里应用 patch + 用 `go-udiff` 算 unified diff；Execute 实际写盘，并在 `FileChange.UnifiedDiff` 中返回实际变更，TUI 默认只展示摘要，按 `Ctrl+O` 展开最近的 tool 区域后先展示工具摘要，再按一次展开最近工具项的着色文件级详情；也可用 `Ctrl+N` / `Ctrl+P` 移动活动焦点并用 `Enter` / `Space` 操作任意活动块或工具项；TUI 默认不启用鼠标追踪，保留终端原生拖拽选择复制。`multiedit`（一次调用跨文件多处编辑）共用 `applyEdit` 与 `udiff`，在内存中按数组顺序对同 path 串行累加，先对所有目标做 TOCTOU 二次读校验再批量写盘，任一步失败即不写盘（写过的文件回滚到 before 快照），从而对调用方提供 all-or-nothing 语义
- `bash`：用 `os/exec` 拉子进程，stdout/stderr 流式回传；超时默认 120s；不实现 Preview（命令是黑盒）
- `job_run`：返回 `job_id`，进程交给后台 goroutine 管理；`job_output` 读流；`job_kill` SIGTERM/SIGKILL
- tool 参数解析对模型常见 JSON 标量抖动做窄容错：整数参数接受整数或整数字符串，布尔参数接受布尔值或 `"true"` / `"false"`，但 JSON Schema 仍对外声明真实 integer/boolean 类型
- `references` 优先支持 `symbol` + 可选 `path` 的符号名查询，由本地搜索定位候选位置后再调用 LSP；兼容 `file` + `line` + `col` 的位置查询

## 5. Provider 抽象

```go
// internal/provider/provider.go
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
- 自动 summary 按 `estimated_input + context.reserve_output_tokens > max_context * context.trigger_ratio` 触发；TUI 的 `/compact` 可主动触发同一 summary 逻辑。最近原文保留使用 `context.keep_recent_turns` + 最近上下文 token budget，至少保留当前 user turn，且按完整 user turn 边界截断，避免孤立 tool_use/tool_result
- tool result 在进入下一次 provider 请求和写入 rollout 前统一限幅：默认最多 12KiB/400 行模型可见内容；超限时完整输出写入 `$XDG_STATE_HOME/ub/tool_outputs/<session>/<tool_use>.txt`（否则 `~/.local/state/ub/...`），rollout 只保存 preview、`truncated`、`original_bytes`、`full_output_path`
- 读取 rollout 历史时遇到 `Summary` 事件即从该 summary message 重新开始构造上下文，避免恢复 session 后重新带上已压缩旧消息

**重要的内部消息表示**：
不要直接复用 anthropic / openai 的请求类型。在 `internal/message/` 自定义中性 `Message` 结构（`Role`、`Content[]`、`ToolCalls[]`、`ToolResults[]`），各 provider 各自转换。理由：避免被某家 SDK 锁定。

## 6. Rollout（事件日志）

```go
// internal/rollout/event.go
type Event struct {
    ID        string
    SessionID string
    Turn      int
    Time      time.Time
    Type      EventType   // user / assistant / tool_call / tool_result / summary / model_switch / mode_switch / perm / error
    Payload   json.RawMessage
}
```

**存储**：SQLite 表 `events(id, session_id, turn, time, type, payload BLOB)`，按 `(session_id, turn, time)` 建索引。写入策略：单条 `INSERT` 即 commit；DB 启用 `PRAGMA journal_mode=WAL` + `PRAGMA synchronous=NORMAL`。

**清理**：启动时做 best-effort 自动清理，默认最多每 24h 运行一次（记录在 `$XDG_STATE_HOME/ub/cleanup.json`，否则 `~/.local/state/ub/cleanup.json`）。默认删除 30 天未更新且不属于其 workspace 最近 20 个的 session；`events` 不单独按条数/大小裁剪，只随 `sessions` 的 `ON DELETE CASCADE` 删除，避免破坏历史恢复、summary 和 rollout replay。tool-output spillover 文件按 `context.tool_results.spillover_max_age` 清理，失败只 warning。启动清理失败只写 warning，不阻断 CLI/TUI 主流程；默认不执行 SQLite `VACUUM`，避免启动时长时间阻塞。

**耐久性目标**（与 requirements F-SESS-4 对齐）：进程崩溃（panic / OOM / SIGKILL）不丢已 commit 的事件；操作系统断电可能丢最后若干条尚未刷盘的事件。**不**为此牺牲性能逐条 fsync——agent 一轮会写数十条事件，每条 fsync 在 SSD 上也要几毫秒，TUI 流畅度会肉眼可见地下降。

**用途**：
1. **会话恢复**：重启后 reader 把事件还原为内存中的 `[]Message`
2. **调试**：`ub rollout show <session>` 漂亮打印整轮 trace
3. **vcr 替代品**：录制真实跑过的 session 后，可在测试里重放（结合 §10）
4. **未来 audit/导出**

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
small_model: openai/gpt-4o-mini   # 用于 summary、生成标题、approval fallback
execution_mode: work               # work / plan / auto

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
  always_allow:
    - tool: bash
      command_prefix: "git status"
    - tool: bash
      command_prefix: "ls "

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

`cleanup.enabled` 区分 unset 与显式 `false`，因此用户可以在全局或 profile 中关闭默认开启的清理。日志轮转在打开 log 文件前执行：超过阈值时 `ub.log` 变为 `ub.log.1`，已有历史依次后移，超过 `max_backups` 的旧文件删除；不压缩日志，`max_size_mb <= 0` 或 `max_backups < 0` 视为关闭轮转。`UB_LOG_FILE` 与 TUI 默认日志路径走同一套轮转逻辑。

## 9. 执行模式与权限模型

`execution.Mode` 是 session 级策略，不等同于 provider profile。profile 决定模型/配置，mode 决定 tool call 能否落地。

```go
type Mode string
const (
    ModeWork Mode = "work"
    ModePlan Mode = "plan"
    ModeAuto Mode = "auto"
)

type ModePolicy struct {
    AllowWrite bool
    ExecPath   ExecApprovalPath // human / approval-agent-with-human-fallback
}
```

**三种模式**：
- `work`：允许 workspace 内文件读写；`exec` 风险工具如果没有命中 allow-rule，走用户审批。
- `plan`：只读规划；`write` 风险工具在 dispatcher 层直接拒绝并把错误回灌给模型；`exec` 风险工具仍走用户审批，审批弹窗必须提示 Plan 模式下命令可能有副作用。
- `auto`：文件读写策略同 `work`；`exec` 风险工具先交给 approval agent 自动判断，若拒绝、不确定或异常，再回退到用户显式审批。

**approval agent** 是一个受限的二级 agent，只输出 `allow` / `deny` / `unsure` 与一句理由，不执行工具、不修改上下文、不写文件。它的输入只包含命令文本、cwd、风险等级、当前 mode、最近相关上下文摘要和已命中的规则信息；API key 等 secret 不传入。黑名单命令不进入 approval agent，直接走用户确认。

```go
type Decision int
const (
    Allow Decision = iota
    Deny
    AlwaysAllowCommand   // session 内：同 tool + 同参数自动放行（内存）
    AlwaysAllowTool      // session 内：同 tool 全放行（内存）
    AlwaysAllowGlobal    // 跨 session 持久化到 ~/.config/ub/permissions.yaml
)

type ApprovalAgent interface {
    ReviewCommand(ctx context.Context, req Request) (ApprovalAgentDecision, error)
}
```

**两层规则存储**：
- **session 级**（`AlwaysAllowCommand` / `AlwaysAllowTool`）：内存 map，agent 进程退出即丢
- **global 级**（`AlwaysAllowGlobal`）：序列化到 `~/.config/ub/permissions.yaml`，启动时加载到内存
  ```yaml
  # ~/.config/ub/permissions.yaml — 由 TUI 写入，用户也可手改
  global:
    - tool: bash
      command_prefix: "git status"
    - tool: fs.edit         # 整工具放行
  ```

UI 流程：
1. tool dispatcher 收到 call，先执行 mode gate：`plan` 模式拒绝所有 `write` 风险工具
2. 若工具实现 PreviewableTool，先调 `Preview()`
3. 对 `exec` 风险工具先查 global rules → 再查 session rules（match 则直接 Allow；黑名单除外）
4. 不 match 时按 mode 选择审批路径：`work`/`plan` 直接向 TUI 发 `PermissionRequest{Call, Preview}`；`auto` 先调 approval agent，并用 `slog` 记录 `allow`/`deny`/`unsure` 或错误原因；返回 `deny`/`unsure`/error 时再向 TUI 发请求
5. TUI 弹 modal，以候选列表展示 5 个选项（与 F-PERM-3 对齐），每个选项都说明作用范围；上/下方向键移动，Enter 确认，`1`~`5` 仅作为快捷键：
   - Allow once：只允许本次请求，不保存规则
   - Deny：拒绝本次请求，不保存规则
   - Always allow this exact command (session)：本 session 内允许完全相同 command
   - Always allow this tool (session)：本 session 内允许同一 tool 的后续调用
   - Always allow this tool (global)：写入 `~/.config/ub/permissions.yaml`，后续 session 生效
6. 决策写入 rollout（`PermissionDecision` 事件，含 `source=rule|approval_agent|human`）；选 3/4 更新内存 rules；选 5 同时追加到磁盘 yaml
7. dispatcher 拿到决策继续

**approval 模型切换规划**：`/approval-model [model]` 只影响 auto 模式的命令审批模型，不改变主对话模型。无参数时展示 approval provider 的候选模型；显式指定时必须通过候选列表校验；切换成功后重建 `permission.Manager` 内的 approval agent，并仅影响后续 tool approval。

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
| LSP | `gopls.dev/protocol` 或自家精简实现 | 仅用 diagnostics/references |
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
2. 配 fake provider 跑单测：`go test ./internal/agent/...`
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
