# ub (Ulimited Blade) — 需求文档

> 状态：v0.1 — 立项讨论结论。所有"V1"指 MVP（首个端到端可用版本）。

## 1. 项目背景与定位

`ub` 是一个跑在终端里、由 LLM 驱动的通用编程 Agent (TUI)。同类产品有 Charm `crush`、OpenAI `codex`、Anthropic `claude-code`、sst `opencode`、`aider` 等。

**核心定位**：**学习/研究向**通用编程助手。不追求与上述产品在功能或生态上正面竞争，而是通过自己实现一个完整的 coding agent 来掌握：

- Agent loop（多轮 tool use 调度）
- LLM Provider 抽象与多模型协作
- 终端 UI（Bubble Tea）
- MCP / LSP 集成
- 会话持久化与可重放事件日志（rollout）
- 上下文窗口管理与自动压缩
- 工具调用的权限模型

**不是什么**：
- 不是 Crush 替代品
- 不是给团队/企业的解决方案
- 不是 Cursor/Windsurf 这类 IDE 集成

## 2. 目标用户

- 项目作者本人（首要）
- 想读懂参考代码的工程师（次要）

## 3. 范围

### 3.1 In Scope（V1）

| 模块 | 内容 |
|---|---|
| 对话 | 多轮、流式输出、可中断 |
| Provider | Anthropic Claude（官方 SDK）、OpenAI（官方 SDK）、OpenAI 兼容协议（含 Ollama `/v1`） |
| Tools | read / write / edit（含 diff 预览）、bash（权限审批）、grep / glob / ls、job_run / job_output / job_kill、plan_write / plan_update / plan_update_step、todo_write / todo_update |
| 权限 | 交互式 allow / deny；支持 always-allow 规则 |
| 执行模式 | `work` / `plan` / `auto` / `full-access` 四种模式，控制文件写入与命令审批路径 |
| 会话 | SQLite 持久化、可列出 / 切换 / 恢复 |
| Rollout | 每一轮 user / assistant / tool_call / tool_result 全部 append-only 写入；可重放调试 |
| 上下文 | 自动 summary / 压缩；接近 context window 极限时触发 |
| MCP | 接入外部 MCP server（http / stdio / sse），扩展工具集 |
| LSP | 接入 LSP，向模型提供 diagnostics 与 references |
| TUI | Bubble Tea 实现的 chat UI、diff view、权限弹窗、状态栏 |
| 配置 | YAML 配置文件（JSON Schema 校验）；运行时通过 slash 命令切换 provider/model/mode/effort |
| 测试 | 单元测试 + 基于 vcr 的 LLM 请求录制 / 回放 |

### 3.2 Out of Scope（V1，可能 V2+）

- 跨平台完整沙箱（Linux bwrap、macOS sandbox-exec、Windows AppContainer）
- 客户端 / 服务端分离（V1 单进程，但 core 不依赖 TUI，预留拆分）
- IDE 插件 / Web Console
- 通用多 agent 协调（codex 的 `collaboration-mode-templates`、claude-code 的 `coordinator`；不含命令审批专用 approval agent）
- skills、hooks 用户自定义系统
- 语音、远程会话、AI 伴侣 UI
- 企业级功能（SSO、审计、Slack 集成）

### 3.3 Out of Scope（永远）

- 自有云端服务
- 付费模型订阅 / 网关代付
- 移动端

## 4. 功能需求

### 4.1 对话

- F-CHAT-1：用户在 TUI 输入框输入消息，回车发送
- F-CHAT-2：模型回复支持流式逐 token 渲染；provider 明确返回的 reasoning/thinking 摘要 MUST 作为活动消息展示，不得混入 assistant 正文或伪造隐藏思维链
- F-CHAT-3：Ctrl+C 中断当前轮（不退出程序）；再次 Ctrl+C 退出
- F-CHAT-4：支持多行输入（Shift+Enter 或外部编辑器）
- F-CHAT-5：历史消息可上下滚动

### 4.2 Provider

- F-PROV-1：默认实现三种 provider：`anthropic`、`openai`、`openai-compat`（Ollama 通过 `/v1` 端点走 `openai-compat`）
- F-PROV-2：每个 provider 实现统一接口：`Chat(ctx, request) -> stream`、`SupportsTools()`、`Caps() ProviderCaps`（含 max context、是否支持 prompt cache 等）
- F-PROV-3：运行时可通过命令（如 `/model openai gpt-4o`）切换，会话上下文保留
- F-PROV-4：API key 从环境变量或配置读取，配置文件支持 `${ENV_VAR}` 引用
- F-PROV-5：**所有 provider 均可覆盖 `base_url`**，用于走第三方网关 / 代理 / 自建反代（LiteLLM、Cloudflare AI Gateway、Helicone、OneAPI、自建 Anthropic 兼容服务等）；未配置则使用 SDK 默认 endpoint
- F-PROV-6：所有 provider 均可自定义额外 HTTP headers（如 `x-org-id`、自家网关鉴权头）和超时
- F-PROV-7：provider 事件流可选返回 `reasoning_delta`；只有后端 API 提供可展示 reasoning/thinking 时才透传，未提供时不得合成
- F-PROV-8：系统 MUST 通过 provider 模型列表发现、内置 ModelInfo 表和用户配置覆盖解析模型 reasoning 能力；只有当前模型声明支持时才向 provider 发送 reasoning effort
- F-PROV-9：OpenAI provider 使用 `reasoning_effort`；Anthropic provider 使用 `thinking` budget；OpenAI-compatible 对未知模型默认不发送 reasoning 参数

### 4.3 工具系统

- F-TOOL-1：工具以接口形式注册（`Name() string`、`Schema() jsonschema`、`Execute(ctx, args) Result`、`Risk() RiskLevel`）
- F-TOOL-2：V1 必备工具列表见 §3.1
- F-TOOL-3：MCP 工具与本地工具走同一接口，从 namespace 区分
- F-TOOL-4：工具结果可以是文本、文件 diff、错误，统一进入消息流
- F-TOOL-5：Agent loop MUST NOT 使用小的固定默认轮数截断正常工具链路；`max_turns` 未设置时默认不按步数截断，只有 `max_turns > 0` 才启用 hard guard。系统 MUST 对重复相同 tool-call/result 的循环做基础检测，并在命中时发起一次禁用工具的收尾请求，避免无限重复。
- F-TOOL-6：系统 MUST 提供 `todo_write` / `todo_update` 维护当前 session 的执行清单；todo 状态为 `pending` / `in_progress` / `completed` / `skipped` / `failed`，同一清单最多一个 `in_progress`。todo 工具 MUST 与 plan artifact 分离，不直接修改 plan markdown checkbox；其 tool result MUST 进入 rollout，并且 TUI MUST 将其渲染为独立 Todo view 而不是只藏在 tool block 详情中，以便 live view、resume 和调试重建清单。
- F-TOOL-7：TUI MUST 提供 plan artifact review/edit 入口，允许用户从 plan tool 完成摘要中看到 `plan_id`，也允许通过当前 workspace 的 plan picker 选择 artifact，或按 `plan_id` 打开 state-root 下的 plan markdown 并在外部编辑器中修改，编辑完成后返回 TUI。

### 4.4 执行模式

- F-MODE-1：每次运行 MUST 有一个 `execution_mode`，可选值为 `work`、`plan`、`auto`、`full-access`；启动参数、配置和 TUI slash 命令均可切换（优先级：CLI flag > profile > config 默认值）。mode 不随 session 持久化，resume 后使用本次运行的有效 mode
- F-MODE-2：`work` 模式允许 agent 在当前 workspace 内读写文件；执行 `exec` 风险工具（`bash` / `job_run` / `job_kill`）时，若未被 session/project allow-rule 明确放行，MUST 弹出用户审批
- F-MODE-3：`plan` 模式为只读规划模式；agent 只可使用只读调研工具（`read` / `ls` / `glob` / `grep`）以及计划工具（`plan_write` / `plan_update`）。`write` 与 `exec` 风险工具、sub-agent、memory、LSP/MCP 等其它工具 MUST NOT 在 plan 模式广告；若模型误调，MUST 被拦截并以 tool error 回灌给模型。
- F-MODE-4：`auto` 模式允许一个额外的 approval agent 自动审批命令；若 approval agent 拒绝、无法判断或调用失败，系统 MUST 回退到用户显式审批，不能静默执行；approval agent 的决策与原因 MUST 写入结构化日志
- F-MODE-5：`full-access` 模式允许 workspace 内文件写入与 `exec` 风险工具默认执行，不调用 human approval 或 approval agent；危险命令黑名单、project deny 规则和 project ask 规则 MUST 仍优先于 full-access 自动放行。当前版本进入 `full-access` 依赖 CLI/config/TUI slash/快捷键的显式切换，不额外要求首次高风险确认 dialog
- F-MODE-6：危险命令黑名单优先级高于所有模式；即使 allow-rule、approval agent 或 full-access 模式放行，仍 MUST 要求用户显式确认
- F-MODE-7：当前执行模式 MUST 显示在 TUI 状态栏，并用于本次运行的工具权限判断

### 4.5 权限审批

- F-PERM-1：每个工具声明 `RiskLevel`：`safe`（read/ls/grep/glob）/ `write`（edit/write）/ `exec`（bash/job_run）
- F-PERM-2：`safe` 默认自动允许；`write` 与 `exec` 的处理由当前执行模式决定
- F-PERM-3：审批 UI 选项：`allow once` / `deny` / `always allow this exact command (session)` / `always allow this tool (session)` / `always allow this exact command (project, persist to disk)` / `always allow similar command (project, persist to disk)`
- F-PERM-4：project 级规则持久化到 `<workspace>/.ub/permissions.yaml`，格式为 `permissions.allow` / `permissions.ask` / `permissions.deny` 字符串数组，规则语法为 Claude-style `Tool(pattern)`；`Bash(cmd)` 精确匹配，`Bash(cmd:*)` 匹配命令前缀；session 级 always-rule 仅内存生效
- F-PERM-4a：命令规则 MUST 拆分 compound command（如 `&&`、`;`、管道、换行）；allow 自动放行要求每个子命令都命中 allow rule，deny 规则命中任意子命令即拒绝，ask 规则命中时必须回退人工确认
- F-PERM-5：危险命令模式匹配黑名单（`rm -rf /`、`mkfs.*` 等）即使匹配 always-rule 也强制再次确认
- F-PERM-6：approval agent、human approval 和 full-access mode 的决策、来源和原因 MUST 作为对话活动消息展示，便于用户理解命令为何被放行或回退

### 4.6 会话与 Rollout

- F-SESS-1：每个工作目录可有多个 session；session 默认按时间命名，可改名
- F-SESS-2：`ub` 启动时列出最近 session，可继续或新建
- F-SESS-3：Session 元数据 MUST 持久化当前主对话 provider 与 model；恢复 session 时 MUST 还原二者，避免把旧 session 的 model 发给当前默认 provider
- F-SESS-4：Rollout 事件类型：`user_message`、`assistant_message`（可包含 `tool_use` content block）、`tool_result`、`summary`、`usage`、`activity`、`memory_write`、`file_history_snapshot`、`error`
- F-SESS-5：Rollout 以 JSONL 写入 SQLite 的 BLOB 列；SQLite 开启 WAL + `synchronous=NORMAL`。**耐久性保证**：进程崩溃（panic / OOM / SIGKILL）不丢已 commit 事件；操作系统断电可能丢最后若干条未刷盘事件——这是可接受的，不为此牺牲性能去逐条 fsync
- F-SESS-6：CLI 子命令 `ub rollout show <id>` 可漂亮打印一轮事件流，并展开 `assistant_message` 中的结构化 content block（包括 `tool_use` 的调用 id、工具名与 input JSON）
- F-SESS-7：TUI `/rewind` MUST 允许用户选择历史 user message 并回到该消息之前：删除当前 session 中目标 turn 及之后的 rollout events，重建对话上下文、TUI transcript 和 context 状态，并把目标 user message 放回输入框。Agent MUST 在每个 user turn 开始前创建文件 checkpoint，并在 `write` / `edit` / `multiedit` 与可安全解析为字面路径的 `bash` 删除（`rm` / `git rm`）真正执行前记录目标文件的旧状态；TUI MUST 根据当前 workspace 与目标 checkpoint 判断受影响文件，让用户选择只回退对话或同时回退 checkpoint 中可恢复的文件。变量、通配符、命令内 `cd` 等无法可靠解析的 shell 路径 MUST 不进入文件历史；checkpoint 缺失可靠旧状态的文件 MUST 跳过并提示，不得静默覆盖无法验证的当前 workspace 改动。
- F-SESS-8：启动时 MAY 执行 best-effort 自动清理：默认删除 30 天未更新且不属于对应 workspace 最近 20 个的 session；events MUST 只随 session 删除级联清理，不做单 session 内局部裁剪，用户显式 `/rewind` 的目标 turn 后截断除外

### 4.7 上下文管理

- F-CTX-1：每次发请求前估算 token 数（按 provider 计费方式），估算 MUST 包含 provider 请求里的工具 schema
- F-CTX-2：当前 turn + history + 预留输出 token 超过 `context_window * threshold`（默认 0.8）时，自动触发 summary；如果 provider 请求仍返回可识别的上下文超限错误，Agent MUST 强制执行一次同一 summary 策略并重试同一轮请求，重试后仍失败则返回 provider 原始错误
- F-CTX-3：Summary 由小模型（配置可指定）生成；summary 输入 MUST 按完整 user turn 边界和 summary 模型 context 预算分块/合并，不得按单条 message 或字符切碎语义单元；单个 user turn 超出 summary 模型预算时 MUST 返回明确错误；摘要替换早期消息，最近原文按 `keep_recent_turns` 与 token budget 共同保留，且不得留下孤立 tool_use/tool_result
- F-CTX-4：Summary 事件本身写入 rollout；恢复 session 时可见 transcript MUST 保持完整且不渲染 summary 为普通消息，下一次 provider 请求的 context history MUST 从最新 summary 的 compacted messages 起步
- F-CTX-5：TUI 可通过 `/compact` 主动触发一次 summary/压缩；手动触发复用同一 summary 策略，但不依赖自动阈值
- F-CTX-6：Agent 发请求前向 TUI 上报估算 token 使用量，provider 返回 usage 后上报最近实际 input token；TUI MUST 区分 `ctx est` 与 `ctx last`，且普通 usage 校准不得伪装成压缩导致的下降
- F-CTX-7：Agent 发起 provider 请求时 MUST 携带当前 runtime context（workspace cwd、shell、OS 与路径规则），但该上下文 MUST NOT 写入 rollout 历史，避免恢复 session 后累积过期路径
- F-CTX-8：模型可见 tool result MUST 按 `context.tool_results` 做统一限幅；超限时完整输出写入 ub state 的 tool-output 文件，rollout 只保存模型可见 preview 与 truncation metadata，恢复 session 不得重新灌入完整大输出

### 4.8 配置

- F-CFG-1：默认配置位于 `~/.config/ub/config.yaml`；工作目录可有 `.ub/config.yaml` 覆盖
- F-CFG-2：配置项：`providers`、`default_provider`、`default_model`、`small_model`（用于 auto memory/title 与 approval fallback，不用于默认 compact summary）、`execution_mode`、`reasoning`、`approval_agent`、`tui`、`permissions`、`mcp_servers`、`lsp_servers`、`context`、`cleanup`、`profiles`；`providers.<name>.models.<model>` 可声明 reasoning 能力和 `max_context_tokens`；`context` 支持 `reserve_output_tokens` 与 `tool_results`
- F-CFG-3：`default_model` 与 `approval_agent.model` 可省略；当 provider 能列出模型时，启动时 MUST 自动选择该 provider 返回的第一个可用模型；provider 无法列模型且运行时要求 model 时，MUST 给出明确配置错误
- F-CFG-4：配置 schema 用 JSON Schema 描述，IDE 可补全
- F-CFG-5：配置支持全局 `reasoning.effort`、`approval_agent.reasoning.effort` 和 `providers.<name>.models.<id>` 能力覆盖；effort 值为 `none|minimal|low|medium|high|xhigh`
- F-CFG-6：（V2）配置变更可通过 `/config reload` 热加载，无需重启。V1 改配置必须重启进程

### 4.9 MCP

- F-MCP-1：支持 `stdio` / `http` / `sse` 三种传输
- F-MCP-2：启动时自动连接配置的 server，工具列表合入主工具表
- F-MCP-3：MCP server 异常不影响主流程（fail-open，记录错误）
- F-MCP-4：MCP 工具调用与本地工具一样走权限审批

### 4.10 LSP

- F-LSP-1：可配置多个 LSP server（按文件类型）
- F-LSP-2：模型可通过 `diagnostics` 工具拿到当前文件错误 / 警告
- F-LSP-3：模型可通过 `references` 工具按符号名（优先）或文件位置拿到符号引用位置
- F-LSP-4：文件被 edit/write 工具修改后，主动 `didChange` 通知 LSP，等下一次 diagnostics 刷新

### 4.11 TUI

- F-TUI-1：主界面：聊天区（80%）+ 状态栏（model / effort / mode / context used/max/% / cwd）
- F-TUI-2：输入框支持多行编辑、历史输入浏览、命令补全（`/` 开头）；Tab 用于补全候选，Shift+Tab 用于切换执行模式（包括运行中和审批弹窗中）；Esc 中断当前操作而不是退出；聊天区支持 PageUp/PageDown 滚动历史输出；TUI 默认不启用鼠标追踪，终端内直接拖拽选择文字 MUST 可用于复制；中文/日文等 IME 预编辑必须跟随输入框真实光标，不得漂移到状态栏或其他 footer 行
- F-TUI-3：Diff 渲染：以 split 或 unified 模式预览 edit 操作；write / edit 工具完成后的活动摘要默认折叠，`Ctrl+O` 展开最近工具组后 MUST 只显示文件级变更摘要，再按一次 `Ctrl+O` MUST 展开最近 write / edit 工具项的文件级变更详情（优先 unified diff）；用户 MUST 可用 `Ctrl+N` / `Ctrl+P` 在多个活动块与工具项之间移动焦点，并用 `Enter` / `Space` 折叠或展开当前焦点；diff 元信息、增删行必须有明显着色
- F-TUI-4：权限弹窗：阻塞式 modal，列出工具名、参数预览、风险等级
- F-TUI-5：命令：`/provider`、`/model`、`/approval-model`、`/small-model`、`/effort`、`/mode`、`/compact`、`/init`、`/rewind`、`/btw`、`/clear`、`/new`、`/help`、`/config`、`/sessions`、`/resume`、`/quit`、`/exit`；`/provider` 可在 TUI 内切换当前主对话 provider，并可同时指定目标 model；不指定 model 时 MUST 优先保留目标 provider 可用的当前 model，否则选择目标 provider 的默认/配置模型；切换结果 MUST 写回当前 session 元数据；`/small-model [model]` MUST 只切换当前进程内 auto memory 使用的当前 provider 模型，不改变主对话 model、compact summary model，也不写回配置文件；无参数时 MUST 展示候选列表，显式指定时 MUST 校验候选模型；`/compact` 主动压缩当前 session 上下文，默认使用当前主对话模型生成摘要；`/init` MUST 启动一轮普通 agent 任务来调研当前 workspace 并创建或改进 `AGENTS.md`，已有文件 MUST 尽量保留准确人工内容并通过常规 edit/write 工具产生可见过程和 diff；`/rewind [turn]` MUST 打开或定位历史 user message 选择器，回到目标消息之前并把该消息放回输入框；`/btw [question]` MUST 打开独立的内存 BTW 视图，带 question 时启动一次独立无工具旁路模型请求，复用当前会话上下文的 text-only 摘要和 BTW 视图内已有 Q/A 作为临时 side history，但不写入主 transcript、conversation history 或 rollout，且运行中不得取消或排队到当前 Agent turn；BTW 请求 MUST 不携带主历史中的 tool_use/tool_result 块，且 provider tool-call 事件或伪工具调用标记 MUST 按错误处理；BTW 视图内 MUST 支持继续输入追问、按普通助手消息规则 Markdown 渲染回答、复制最新答案、清空当前 BTW 记录，并使用独立滚动状态查看长输出；BTW 底部 MUST 使用专属状态行而非主对话状态栏，并以 `answering` / `idle` 展示模型回答中或空闲；`Esc` MUST 退出 BTW 视图并清空该视图的临时 Q/A 与草稿；`/clear` 只清空当前聊天区显示；`/new` 创建并切换到新的空 session，同时清空本地消息、排队输入和 context 状态栏；`/sessions` 可切换当前 workspace 的历史 session 并支持 `search <query>` 搜索历史事件；`/resume` 只用于恢复历史 session，无参数打开 session picker，带 session id 时直接恢复；`/effort` 只允许选择当前模型支持的思考等级；`/help` MUST 同时列出 slash 命令、输入前缀、键盘快捷键、picker/permission 快捷键和复制相关行为
- F-TUI-6：TUI 启动支持 `ub --provider <name>` 与 `ub --model <id>` 临时覆盖当前主对话 provider/model；支持 `ub --resume` 打开当前 workspace 的历史 session 选择器，支持 `ub --resume=<id>` 或 `ub --resume <id>` 恢复指定 session
- F-TUI-7：TUI MUST 在聊天区以紧凑活动行展示 thinking、工具排队/运行/完成/失败、审批结果和错误摘要；thinking 与 tool activity MUST 按同一 Agent turn 拆成两个独立可折叠区域展示，审批结果归入 tool 区域；同一轮连续 thinking delta MUST 合并到 thinking 区域，并在折叠摘要中展示可读片段；同一个 tool call 的状态更新 MUST 合并到 tool 区域的同一行，避免 queued/running/done 刷屏；活动行参与宽度换行与聊天区滚动，且不得展示完整工具 JSON 或 secret 值
- F-TUI-8：Agent turn 运行中输入普通消息并按 Enter 时，TUI MUST 将该消息加入本地队列而不是启动并发 Agent turn；当前 turn 正常结束后 MUST 按 FIFO 自动发送下一条队列消息。运行中上下方向键 SHOULD 优先浏览并编辑已排队消息；slash 命令输入不得作为队列消息发送；`/btw [question]` 是例外，运行中 MUST 立即启动旁路请求且不影响主 turn
- F-TUI-9：输入首个非空字符为 `!` 时，TUI MUST 直接在当前 workspace 执行后续 shell 命令；该命令不经过模型、不弹权限审批、不写入 rollout/history，输入区 MUST 显示 shell 模式提示，结果 MUST 作为本地输出直接显示，不渲染成模型 tool 调用
- F-TUI-10：输入中出现 `@` 文件引用时，TUI SHOULD 展示当前 workspace 普通文件候选；选择候选后只把 `@relative/path` 插入输入框，不自动读取或注入文件内容

### 4.12 开发模式与环境诊断

- F-DEV-1：内置 `fake` provider，可在测试与脚本驱动场景下按预设脚本返回 text/reasoning/tool_call/done 事件，**完全离线、零 API 消耗**
- F-DEV-2：配置文件支持 `profiles:` 节，每个 profile 可覆盖 `default_model`、`small_model`、`execution_mode`、`tools_disabled`、`permissions` 等任意运行时项
- F-DEV-3：`ub run --profile <name>` 选择 profile；`--dev` 是 `--profile dev` 的别名；`UB_PROFILE` 环境变量同效
- F-DEV-4：`dev` profile 默认指向用户本地推理服务（vLLM / Ollama / llama.cpp / LM Studio / 内网 Together），通过 `base_url` 配置，**全部走 `openai-compat`**
- F-DEV-5：`ub doctor` 子命令体检本地环境，输出红绿灯报告：
  - 各 provider 的 base_url 是否可达
  - 各 provider 下当前可用模型列表（对支持 `/v1/models` 端点的拉一次）
  - 哪些模型声明支持 tool calling 和 reasoning effort（按内置 ModelInfo 表 + 用户覆盖）
  - 外部命令存在性：`rg`、`gopls`、`typescript-language-server`、`npx`
  - 已配置 MCP server 启动连通性
  - API key 环境变量是否就位
  - 可选 `--suggest`：输出建议的 `profiles.dev` 配置片段供复制

## 5. 非功能性需求

| 类别 | 要求 |
|---|---|
| 性能 | 启动 < 200ms；流式渲染无明显卡顿；后台 LLM 调用不阻塞 TUI |
| 跨平台 | Linux、macOS 主力支持；Windows 至少可跑（不保证完美） |
| 可测试 | 核心 agent loop / tool / provider 单元测试覆盖；vcr 录制集成测试 |
| 可观测 | `slog` 结构化日志；`UB_LOG_LEVEL`、`UB_LOG_FILE` 环境变量；TUI 默认写入用户 state 目录日志文件；默认按 10MB x 5 做日志轮转；可选 pprof |
| 安全 | `exec` 工具默认需审批；`plan` 模式拒绝写工具；API key 不出现在日志和 rollout 中 |
| 兼容性 | 单二进制分发；无运行时依赖（除 LSP/MCP server 用户自备） |

## 6. 路线图

路线图的权威来源是 [`roadmap.md`](./roadmap.md)，按 35 个迭代 + 6 个 Sprint 组织。这里仅给出版本到 Sprint 的映射，避免双源漂移：

| 版本 | 对应 Sprint | 主要交付 |
|---|---|---|
| V0（脚手架） | Sprint 0（I-01 ~ I-04） | 仓库骨架、CLI、配置加载、SQLite、日志 |
| V1（MVP） | Sprint 1 ~ 5（I-05 ~ I-32） | 全部 §3.1 范围；典型端到端：用户说 "帮我修 main.go 里的 typo" → agent read → edit → 显示 diff → 用户确认 → 写盘 |
| V1.1 收尾 | Sprint 6（I-33 ~ I-35） | session resume、`ub rollout show`、第一次 release |
| V2（深化） | 暂未排迭代 | 客户端/服务端拆分（HTTP API + TUI 走 client）；配置热加载（`/config reload`）；并行 tool call；LSP rename / code action |
| V3+（按需） | — | 沙箱（Linux bwrap）；skills / hooks 用户自定义；通用多 agent 协调 |
