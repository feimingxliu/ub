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
| Provider | Anthropic Claude（官方 SDK）、OpenAI（官方 SDK）、OpenAI 兼容协议、Ollama |
| Tools | read / write / edit（含 diff 预览）、bash（权限审批）、grep / glob / ls、job_run / job_output / job_kill |
| 权限 | 交互式 allow / deny；支持 always-allow 规则 |
| 会话 | SQLite 持久化、可列出 / 切换 / 恢复 |
| Rollout | 每一轮 user / assistant / tool_call / tool_result 全部 append-only 写入；可重放调试 |
| 上下文 | 自动 summary / 压缩；接近 context window 极限时触发 |
| MCP | 接入外部 MCP server（http / stdio / sse），扩展工具集 |
| LSP | 接入 LSP，向模型提供 diagnostics 与 references |
| TUI | Bubble Tea 实现的 chat UI、diff view、权限弹窗、状态栏 |
| 配置 | YAML 配置文件（JSON Schema 校验）；运行时通过 slash 命令切换 provider/model |
| 测试 | 单元测试 + 基于 vcr 的 LLM 请求录制 / 回放 |

### 3.2 Out of Scope（V1，可能 V2+）

- 跨平台完整沙箱（Linux bwrap、macOS sandbox-exec、Windows AppContainer）
- 客户端 / 服务端分离（V1 单进程，但 core 不依赖 TUI，预留拆分）
- IDE 插件 / Web Console
- 多 agent 协调（codex 的 `collaboration-mode-templates`、claude-code 的 `coordinator`）
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
- F-CHAT-2：模型回复支持流式逐 token 渲染（含 reasoning content if available）
- F-CHAT-3：Ctrl+C 中断当前轮（不退出程序）；再次 Ctrl+C 退出
- F-CHAT-4：支持多行输入（Shift+Enter 或外部编辑器）
- F-CHAT-5：历史消息可上下滚动

### 4.2 Provider

- F-PROV-1：默认实现四种 provider：`anthropic`、`openai`、`openai-compat`、`ollama`
- F-PROV-2：每个 provider 实现统一接口：`Chat(ctx, request) -> stream`、`SupportsTools()`、`Caps() ProviderCaps`（含 max context、是否支持 prompt cache 等）
- F-PROV-3：运行时可通过命令（如 `/model openai gpt-4o`）切换，会话上下文保留
- F-PROV-4：API key 从环境变量或配置读取，配置文件支持 `${ENV_VAR}` 引用
- F-PROV-5：**所有 provider 均可覆盖 `base_url`**，用于走第三方网关 / 代理 / 自建反代（LiteLLM、Cloudflare AI Gateway、Helicone、OneAPI、自建 Anthropic 兼容服务等）；未配置则使用 SDK 默认 endpoint
- F-PROV-6：所有 provider 均可自定义额外 HTTP headers（如 `x-org-id`、自家网关鉴权头）和超时

### 4.3 工具系统

- F-TOOL-1：工具以接口形式注册（`Name() string`、`Schema() jsonschema`、`Execute(ctx, args) Result`、`Risk() RiskLevel`）
- F-TOOL-2：V1 必备工具列表见 §3.1
- F-TOOL-3：MCP 工具与本地工具走同一接口，从 namespace 区分
- F-TOOL-4：工具结果可以是文本、文件 diff、错误，统一进入消息流

### 4.4 权限审批

- F-PERM-1：每个工具声明 `RiskLevel`：`safe`（read/ls/grep/glob）/ `write`（edit/write）/ `exec`（bash/job_run）
- F-PERM-2：`safe` 默认自动允许；`write`、`exec` 默认弹出审批 UI
- F-PERM-3：审批 UI 选项：`allow once` / `deny` / `always allow this exact command (session)` / `always allow this tool (session)` / `always allow this tool (global, persist to disk)`
- F-PERM-4：session 级 always-rule 仅内存生效；global 级 always-rule 持久化到 `~/.config/ub/permissions.yaml`，下次启动自动加载
- F-PERM-5：危险命令模式匹配黑名单（`rm -rf /`、`mkfs.*` 等）即使匹配 always-rule 也强制再次确认

### 4.5 会话与 Rollout

- F-SESS-1：每个工作目录可有多个 session；session 默认按时间命名，可改名
- F-SESS-2：`ub` 启动时列出最近 session，可继续或新建
- F-SESS-3：Rollout 事件类型：`UserMessage`、`AssistantMessage`、`ToolCall`、`ToolResult`、`Summary`、`ModelSwitch`、`PermissionDecision`、`Error`
- F-SESS-4：Rollout 以 JSONL 写入 SQLite 的 BLOB 列；SQLite 开启 WAL + `synchronous=NORMAL`。**耐久性保证**：进程崩溃（panic / OOM / SIGKILL）不丢已 commit 事件；操作系统断电可能丢最后若干条未刷盘事件——这是可接受的，不为此牺牲性能去逐条 fsync
- F-SESS-5：CLI 子命令 `ub rollout show <id>` 可漂亮打印一轮事件流

### 4.6 上下文管理

- F-CTX-1：每次发请求前估算 token 数（按 provider 计费方式）
- F-CTX-2：当前 turn + history token 超过 `context_window * threshold`（默认 0.8）时，自动触发 summary
- F-CTX-3：Summary 由小模型（配置可指定）生成；摘要替换早期消息，保留最近 N 轮原文
- F-CTX-4：Summary 事件本身写入 rollout，下次恢复 session 可从 summary 起步

### 4.7 配置

- F-CFG-1：默认配置位于 `~/.config/ub/config.yaml`；工作目录可有 `.ub/config.yaml` 覆盖
- F-CFG-2：配置项：`providers`、`default_model`、`small_model`（用于 summary/title）、`tui`、`permissions`、`mcp_servers`、`lsp_servers`、`profiles`
- F-CFG-3：配置 schema 用 JSON Schema 描述，IDE 可补全
- F-CFG-4：（V2）配置变更可通过 `/config reload` 热加载，无需重启。V1 改配置必须重启进程

### 4.8 MCP

- F-MCP-1：支持 `stdio` / `http` / `sse` 三种传输
- F-MCP-2：启动时自动连接配置的 server，工具列表合入主工具表
- F-MCP-3：MCP server 异常不影响主流程（fail-open，记录错误）
- F-MCP-4：MCP 工具调用与本地工具一样走权限审批

### 4.9 LSP

- F-LSP-1：可配置多个 LSP server（按文件类型）
- F-LSP-2：模型可通过 `diagnostics` 工具拿到当前文件错误 / 警告
- F-LSP-3：模型可通过 `references` 工具拿到符号引用位置
- F-LSP-4：文件被 edit/write 工具修改后，主动 `didChange` 通知 LSP，等下一次 diagnostics 刷新

### 4.10 TUI

- F-TUI-1：主界面：聊天区（80%）+ 状态栏（model / context % / cwd）
- F-TUI-2：输入框支持多行编辑、命令补全（`/` 开头）
- F-TUI-3：Diff 渲染：以 split 或 unified 模式预览 edit 操作
- F-TUI-4：权限弹窗：阻塞式 modal，列出工具名、参数预览、风险等级
- F-TUI-5：命令：`/model`、`/clear`、`/help`、`/config`、`/sessions`、`/quit`

### 4.11 开发模式与环境诊断

- F-DEV-1：内置 `fake` provider，可在测试与脚本驱动场景下按预设脚本返回 text/tool_call/done 事件，**完全离线、零 API 消耗**
- F-DEV-2：配置文件支持 `profiles:` 节，每个 profile 可覆盖 `default_model`、`small_model`、`tools_disabled`、`permissions` 等任意运行时项
- F-DEV-3：`ub run --profile <name>` 选择 profile；`--dev` 是 `--profile dev` 的别名；`UB_PROFILE` 环境变量同效
- F-DEV-4：`dev` profile 默认指向用户本地推理服务（vLLM / Ollama / llama.cpp / LM Studio / 内网 Together），通过 `base_url` 配置，**全部走 `openai-compat`**
- F-DEV-5：`ub doctor` 子命令体检本地环境，输出红绿灯报告：
  - 各 provider 的 base_url 是否可达
  - 各 provider 下当前可用模型列表（对支持 `/v1/models` 端点的拉一次）
  - 哪些模型声明支持 tool calling（按内置 ModelInfo 表）
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
| 可观测 | `slog` 结构化日志；`UB_LOG_LEVEL`、`UB_LOG_FILE` 环境变量；可选 pprof |
| 安全 | 默认拒绝 write/exec 工具；API key 不出现在日志和 rollout 中 |
| 兼容性 | 单二进制分发；无运行时依赖（除 LSP/MCP server 用户自备） |

## 6. 路线图

路线图的权威来源是 [`roadmap.md`](./roadmap.md)，按 35 个迭代 + 6 个 Sprint 组织。这里仅给出版本到 Sprint 的映射，避免双源漂移：

| 版本 | 对应 Sprint | 主要交付 |
|---|---|---|
| V0（脚手架） | Sprint 0（I-01 ~ I-04） | 仓库骨架、CLI、配置加载、SQLite、日志 |
| V1（MVP） | Sprint 1 ~ 5（I-05 ~ I-32） | 全部 §3.1 范围；典型端到端：用户说 "帮我修 main.go 里的 typo" → agent read → edit → 显示 diff → 用户确认 → 写盘 |
| V1.1 收尾 | Sprint 6（I-33 ~ I-35） | session resume、`ub rollout show`、第一次 release |
| V2（深化） | 暂未排迭代 | 客户端/服务端拆分（HTTP API + TUI 走 client）；配置热加载（`/config reload`）；并行 tool call；LSP rename / code action |
| V3+（按需） | — | 沙箱（Linux bwrap）；skills / hooks 用户自定义；多 agent 协调 |
- 多 agent 协调
