# ub — 开发路线图（AI 友好的小步迭代）

> 状态：v0.2 — 与 `requirements.md`、`design.md` 配套。每个迭代都是一次可独立完成、可验证的开发会话。

## 编排原则

1. **垂直切片优先**：尽早打通端到端，避免长期写"未被调用"的代码
2. **粒度 = 一次 AI 会话**：每个迭代约 200–500 行代码 + 测试
3. **强约束的迭代描述**：包含 *目标 / 依赖 / In Scope / Out of Scope / 验证 / 关键签名*
4. **测试即验收**：除特殊原因外，每个迭代必须落实 `go test ./...` 通过 + 一条 CLI 冒烟命令
5. **离线开发优先**：先有 fake provider（I-07）再有真实 provider，agent loop / tool / TUI 整条链路在无 API key 下可端到端开发；真实模型测试走本地 vLLM（I-13）；CI 走 vcr 回放（I-06）

## 迭代地图（共 6 个 Sprint，35 个迭代）

| Sprint | 目标 | 迭代 | 完成后能做什么 |
|---|---|---|---|
| 0 基建 | 仓库、CLI、配置、存储 | I-01 ~ I-04 | `ub --version`、`ub config show`、SQLite 可读写 |
| 1 第一次对话 | Provider + Rollout，无 TUI | I-05 ~ I-14 | `ub chat "hi"` 走任一 provider，事件入 rollout，`ub doctor` 体检 |
| 2 工具与 Agent | 本地工具 + 单轮 agent loop | I-15 ~ I-21 | `ub run -p "..."` 调工具端到端 |
| 3 TUI | Bubble Tea 接管交互 | I-22 ~ I-26 | 类 Claude Code 交互 |
| 4 上下文与摘要 | token 估算 + 自动 summary | I-27 ~ I-28 | 长会话不爆 context |
| 5 MCP & LSP | 外部工具与代码感知 | I-29 ~ I-32 | 接入 MCP server、gopls 拿 diagnostics |
| 6 收尾 | resume、rollout 查看、发布 | I-33 ~ I-35 | v0.1.0 release |

---

## Sprint 0 — 基建

### I-01 仓库骨架与 cobra 入口

- **目标**：可 `go build` 出二进制，`ub --version` 输出版本号
- **依赖**：无
- **In Scope**：
  - `go mod init`，确定 module path
  - 目录骨架（`cmd/ub`、`internal/`，先空着）
  - cobra 根命令 + 三个占位子命令：`run`（默认）、`config`、`sessions`
  - `Makefile` 或 `justfile`（`build` / `test` / `lint`）
  - GitHub Actions：`go test`、`go vet`、`gofumpt`（即使 repo 暂无远端也准备好）
  - `.gitignore` 增补 Go 标准条目
- **Out of Scope**：任何业务逻辑、TUI、provider
- **关键签名**：
  ```go
  // cmd/ub/main.go
  func main() { cli.Execute() }
  // internal/cli/root.go
  func Execute()
  ```
- **验证**：
  - `go build ./...` 成功
  - `./ub --version` 打印版本（embed 用 `runtime/debug.ReadBuildInfo`）
  - `./ub run --help`、`./ub config --help`、`./ub sessions --help` 都有输出

### I-02 配置加载与 schema

- **目标**：YAML 配置可加载、可分层覆盖、可校验
- **依赖**：I-01
- **In Scope**：
  - `internal/config/`：`Config` 结构体（按 design §8）
  - YAML 加载（`goccy/go-yaml`）
  - 加载顺序：内置默认 → `~/.config/ub/config.yaml` → `./.ub/config.yaml` → 环境变量
  - `${ENV_VAR}` 替换
  - 生成 `schema/config.schema.json`（用 `invopop/jsonschema`）
  - `ub config show` 子命令：打印合并后的有效配置
  - `ub config path` 子命令：列出本次加载用到的文件
- **Out of Scope**：JSON 配置文件（只用 YAML，JSON Schema 仅作校验）；`profiles:` 节（I-13）；`/config reload` 热加载（V2）；配置写回（V2）
- **关键签名**：
  ```go
  type Config struct {
      DefaultProvider string                    `yaml:"default_provider"`
      DefaultModel    string                    `yaml:"default_model"`
      SmallModel      string                    `yaml:"small_model"`
      Providers       map[string]ProviderConfig `yaml:"providers"`
      Permissions     PermissionConfig          `yaml:"permissions"`
      MCPServers   map[string]MCPServerConfig `yaml:"mcp_servers"`
      LSPServers   map[string]LSPServerConfig `yaml:"lsp_servers"`
      TUI          TUIConfig                 `yaml:"tui"`
      Context      ContextConfig             `yaml:"context"`
  }
  func Load() (*Config, []string /*used files*/, error)
  ```
- **验证**：
  - 单测：env 替换、分层覆盖、缺字段默认值、非法 YAML 报错
  - `ub config show` 在空配置下打印默认；放一个 `~/.config/ub/config.yaml` 后能合并

### I-03 SQLite store 与 schema migration

- **目标**：建表 + 一个最小迁移机制 + sessions 表 CRUD
- **依赖**：I-01
- **In Scope**：
  - `internal/store/`：用 `modernc.org/sqlite`
  - 启动时检测 `schema_version` 表，按顺序执行 `001_init.sql`、`002_*.sql`
  - 表：`sessions`、`events`（见 design §7）
  - `Store` 提供 `CreateSession`、`GetSession`、`ListSessions(workspace string, limit int)`、`UpdateSession`、`DeleteSession`
  - `ub sessions ls` 命令：列出当前工作目录的 session
- **Out of Scope**：events 操作（I-09）；rollout 写入（I-09）；sqlc
- **关键签名**：
  ```go
  type Session struct {
      ID, Workspace, Title, Model string
      CreatedAt, UpdatedAt        time.Time
  }
  type Store interface {
      CreateSession(ctx, Session) error
      GetSession(ctx, id string) (*Session, error)
      ListSessions(ctx, ws string, limit int) ([]Session, error)
      UpdateSession(ctx, Session) error
      DeleteSession(ctx, id string) error
      Close() error
  }
  func Open(path string) (Store, error)
  ```
- **验证**：
  - 单测：CRUD 全路径，migration 幂等
  - `ub sessions ls` 在空库下输出 "no sessions"，手动 insert 一条后能看到

### I-04 日志与 panic recovery

- **目标**：全局 `slog`，CLI 出错可读
- **依赖**：I-01
- **In Scope**：
  - `internal/log/`：基于 `slog`，支持 `UB_LOG_LEVEL`、`UB_LOG_FILE`
  - 默认 stderr 写人类格式；`UB_LOG_FILE` 写 JSON
  - `cli` 顶层 panic recover，打印调用栈并退出
  - 子命令统一 error 渲染（不要 cobra 默认的丑陋格式）
- **Out of Scope**：metrics、tracing
- **验证**：
  - 单测：level 解析、formatter
  - `UB_LOG_LEVEL=debug ./ub config show 2>&1 | grep -i debug` 能看到调试日志

---

## Sprint 1 — 第一次对话（无 TUI）

### I-05 内部 Message 类型

- **目标**：定义中性消息结构，独立于任何 SDK
- **依赖**：I-01
- **In Scope**：
  - `internal/message/`：`Message`、`Role`、`Content`（文本 / 图片 / tool_use / tool_result）
  - JSON 序列化（用于 rollout 存储）
  - 工具函数：`(m Message).Text() string`、`Append(content)`、`Clone()`
- **Out of Scope**：与具体 provider 的转换（在各 provider 包做）
- **关键签名**：
  ```go
  type Role string // user / assistant / system / tool
  type Message struct {
      Role    Role
      Content []ContentBlock
  }
  type ContentBlock struct {
      Type      string          // text / image / tool_use / tool_result
      Text      string          `json:",omitempty"`
      ToolUseID string          `json:",omitempty"`
      ToolName  string          `json:",omitempty"`
      Input     json.RawMessage `json:",omitempty"`
      Output    string          `json:",omitempty"`
      IsError   bool            `json:",omitempty"`
  }
  ```
- **验证**：单测序列化往返、Clone 深拷贝

### I-06 vcr：HTTP 录制 / 回放

- **目标**：测试基础设施。录制时打真实 LLM，回放时按 cassette 应答
- **依赖**：I-01
- **In Scope**：
  - `internal/vcr/`：实现 `http.RoundTripper`
  - 模式：`record` / `replay` / `disabled`，环境变量 `UB_VCR=record|replay`
  - Cassette 格式：JSONL，每行一对 `{request, response}`
  - 脱敏：自动剥离 `Authorization`、`x-api-key` 等 header
  - 请求匹配：method + url + body hash（顺序敏感）
- **Out of Scope**：自动 cassette 重命名、并发请求支持（V2）
- **关键签名**：
  ```go
  type Recorder struct{ ... }
  func New(cassettePath, mode string) *Recorder
  func (r *Recorder) RoundTrip(req *http.Request) (*http.Response, error)
  ```
- **验证**：单测：起一个 `httptest.Server`，record 写 cassette → replay 从 cassette 应答 → 断言一致

### I-07 Provider 接口 + Fake provider + `ub chat` 骨架

- **目标**：Provider 抽象就位 + 一个纯内存的脚本化 provider + 最小可用的 `ub chat` 子命令，让后续 agent loop / tool / TUI 整条链路在**无 API key**下也可端到端开发与单测；同时给 I-08/I-09 的验证一个稳定的 CLI 入口
- **依赖**：I-05
- **In Scope**：
  - `internal/provider/`：`Provider`、`ProviderConfig`、`Caps`、`Request`、`Stream`、`Event` 等核心类型（参见 design §5）
  - 工厂：`provider.New(name, cfg)` 按 `type` 路由
  - `internal/provider/fake/`：
    - 脚本驱动：按预设事件序列依次返回（`text_delta` / `tool_call` / `usage` / `done`）
    - 支持配置加载（`type: fake` + `script: [...]`）与代码内构造（`fake.New(fake.Script{...})`）
    - 多 turn：可配置"看到某 tool_result 后继续发射哪些事件"，用于测多轮 agent loop
  - `ub chat` 子命令最小版：
    - 参数：`ub chat "..."`、`--provider <name>`、`--model <id>`、stdin（`ub chat -`）
    - 读 config → 取对应 provider → 单轮发请求 → 流式 stdout
    - 不带 tools、不写 rollout（rollout 在 I-09 接进来）
- **Out of Scope**：任何真实网络 provider（I-08+）；session/rollout 关联（I-09）；TUI 集成（Sprint 3）
- **关键签名**：
  ```go
  type Provider interface {
      Name() string
      Caps() Caps
      Chat(ctx context.Context, req Request) (Stream, error)
  }
  type Stream interface {
      Next(ctx context.Context) (Event, error)
      Close() error
  }
  // 测试便利构造
  func fake.New(script fake.Script) Provider
  type fake.Script []fake.Event
  ```
- **验证**：
  - 单测：脚本顺序产出；ctx 取消能 close；YAML 加载与代码构造行为一致
  - 冒烟：在 config 里配一个 fake provider → `ub chat --provider fake "hello"` 流式打印脚本里的 text_delta

### I-08 Anthropic provider（非流式）

- **目标**：第一个真实 provider，配置可覆盖 `base_url`
- **依赖**：I-02 / I-06 / I-07
- **In Scope**：
  - `internal/provider/anthropic/`：包 `anthropic-sdk-go`
  - 一次性非流式调用（`Stream` 返回单事件 + done）
  - Message ↔ Anthropic API 双向转换
  - 复用 ProviderConfig：`api_key`、`base_url`、`headers`、`timeout` 全部生效
- **Out of Scope**：流式、tool use、其他 provider
- **验证**：
  - 单测（vcr replay）：固定 prompt → 断言返回包含期望文本
  - 手测：放真 API key，`UB_VCR=record ./ub chat "say only the word PONG"`

### I-09 Rollout 事件写入与读取

- **目标**：每次对话事件落 SQLite，可重读
- **依赖**：I-03 / I-05 / I-08
- **In Scope**：
  - `internal/rollout/`：`Event`、`Type`、`Writer`、`Reader`
  - 先实现事件类型：`UserMessage`、`AssistantMessage`、`Usage`、`Error`
  - SQLite 开启 `journal_mode=WAL` + `synchronous=NORMAL`；单条 INSERT 即 commit；不逐条 fsync
  - 读取：按 session_id 流式遍历
  - 把 I-07 的 `ub chat` 接入：每轮把事件写进 rollout（绑定一个 session）
- **Out of Scope**：tool 相关事件（I-21）、Summary 事件（I-28）、漂亮打印（I-34）
- **关键签名**：
  ```go
  type Writer interface {
      Append(ctx context.Context, e Event) error
      Close() error
  }
  type Reader interface {
      Iter(ctx context.Context, sessionID string) iter.Seq2[Event, error]
  }
  ```
- **验证**：
  - 单测：写 100 条 → 读出来顺序一致、turn 单调递增
  - **耐久性**：起一个 writer 写 N 条 → 用 `os.Exit(1)` 直接干掉进程（不走 close）→ 重开 reader 读到 N 条（验证 SQLite commit 即可见，不需 fsync）
  - 冒烟：跑两次 `ub chat`（fake provider，I-07 已可用），`ub sessions ls` 看到记录，`sqlite3 ub.db "select count(*) from events"` 增加

### I-10 Anthropic 流式

- **目标**：流式逐 token 输出
- **依赖**：I-08
- **In Scope**：
  - 用 SDK 的 `Stream` API
  - `Stream.Next()` 逐 `Event{Type:"text_delta"}` 返回
  - 中断（`ctx.Done()`）能安全关闭
- **Out of Scope**：tool call 流（I-21）
- **验证**：单测（vcr 录一个流式 cassette）：所有 delta 拼接 == 完整 text；中断时 close 不 panic

### I-11 OpenAI Provider（流式 + 非流式）

- **目标**：第二个真实 provider 走通
- **依赖**：I-08 / I-10
- **In Scope**：
  - `internal/provider/openai/`，包 `openai-go`
  - 实现 Provider 接口，沿用 `ProviderConfig`（含 `base_url`）
  - Message ↔ OpenAI ChatCompletion 双向
- **Out of Scope**：tool use（I-21）、Responses API、reasoning content
- **验证**：与 I-08 同等的 vcr 单测；CLI 冒烟切换 `--provider openai`

### I-12 openai-compat 与 Ollama

- **目标**：剩两个 provider，复用最大化
- **依赖**：I-11
- **In Scope**：
  - `internal/provider/compat/`：实质就是 OpenAI provider 包一层，强制要求 `base_url`，用于 vLLM / Together / DeepSeek / LM Studio / Azure 等
  - `internal/provider/ollama/`：用 Ollama 的 REST `/api/chat`（可拿到 reasoning 等元数据）；时间紧也可以走 Ollama 的 `/v1` OpenAI 兼容路径
  - 工厂注册
- **Out of Scope**：本地模型的工具调用质量调优
- **验证**：
  - compat：vcr 录一个 DeepSeek 调用
  - Ollama：本地起 `ollama serve` + `qwen2.5-coder:1.5b` 跑通一次（手测，可选 cassette）

### I-13 Profiles + `--dev` + `--mode` + `ub doctor`

- **目标**：把"开发期接入本地模型测试"做成一等公民
- **依赖**：I-02 / I-08 ~ I-12
- **In Scope**：
  - 配置新增 `profiles:` 节（design §12.2），加载时按 `--profile <name>` / `--dev` / `UB_PROFILE` 选择并叠加
  - 配置新增 `execution_mode` 与 `approval_agent` 字段；`profiles:` 可覆盖 execution mode
  - CLI：`--profile`、`--dev`（= `--profile dev`）、`--mode work|plan|auto` 全局标志位
  - `ub doctor` 子命令（design §12.3）：
    - 探测各 provider 的 `base_url` 可达性（GET `${base_url}/models` 或 Anthropic 轻量 ping）
    - 列出可用模型 + 标注哪些声明支持 tool calling
    - `exec.LookPath` 检查 `rg`、`gopls`、`typescript-language-server`、`npx`
    - `--suggest` 输出建议 `profiles.dev` 配置片段
    - `--plain` 关闭 lipgloss 着色（CI 友好）
- **Out of Scope**：MCP server 连通性检查（依赖 I-29 的 MCP client，待 I-30 之后扩 doctor）；自动写入用户配置文件（V2）
- **关键签名**：
  ```go
  type Profile struct{ /* 与 Config 同结构，所有字段 omitempty */ }
  func (c *Config) ApplyProfile(name string) error
  ```
- **验证**：
  - 单测：profile 叠加（顶层 vs profile 字段）、`--dev` 别名解析、`--mode` 优先级覆盖
  - 集成：起 mock OpenAI 兼容 server，`ub doctor` 能看到 ✓
  - 冒烟：`ub run --dev -p "say hi"` 走本地 vLLM 跑通

### I-14 CLI `chat` 完整化

- **目标**：把 I-07 的最小 `ub chat` 补齐到生产级
- **依赖**：I-07 ~ I-13
- **In Scope**：
  - `ub chat --session <id>` 在已有 session 上继续（叠加 history）
  - `ub chat --provider openai --model gpt-4o-mini "..."` 临时覆盖 default_model
  - 未传 `--provider` 时使用 `default_provider`；没有配置时使用第一个可用 provider，不从 `default_model` 前缀推断
  - `ub chat --new` 强制开新 session
  - 错误处理：provider 不存在 / model 不存在 / 鉴权失败时的人话报错
- **Out of Scope**：TUI 内的 `/model` 命令（Sprint 3）
- **验证**：
  - 单测：参数解析
  - 冒烟脚本：跑三次 chat（fake / Anthropic / vLLM），`ub sessions ls` 看到三个 session

---

## Sprint 2 — 工具与 Agent loop

### I-15 Tool 接口、Registry、Risk、PreviewableTool

- **目标**：工具基础设施就位，没具体工具
- **依赖**：I-05
- **In Scope**：
  - `internal/tool/`：`Tool` 接口、`Result`、`Risk`、`Registry`
  - **可选接口** `PreviewableTool`：写类工具实现它，dispatcher 在 Execute 前调用 Preview，把结果交给 permission UI（见 design §4）
  - `Preview` / `FileDiff` 类型定义
  - JSON Schema 生成（`invopop/jsonschema` 或手写）
  - 注册时检查重名
- **Out of Scope**：具体工具实现、MCP 工具适配
- **关键签名**：见 design §4
- **验证**：
  - 单测：注册 / 查找 / 重名 / Schema JSON 序列化
  - mock 一个实现 `PreviewableTool` 的工具 → 断言 dispatcher 在 Execute 前调用 Preview 一次

### I-16 fs 工具组（read / write / edit / ls / glob）

- **目标**：5 个 fs 工具落地；`write` / `edit` 实现 `PreviewableTool`
- **依赖**：I-15
- **In Scope**：
  - `internal/tool/fs/`：
    - `read(path, offset?, limit?)` → 文本（带行号）
    - `ls(path)` → 文件 / 目录列表
    - `glob(pattern)` → 匹配的路径（用 `doublestar`）
    - `write(path, content)` → 覆盖写；**实现 Preview**：读现盘 → 算 unified diff 返回 `FileDiff{Kind: create|modify}`
    - `edit(path, old, new, replace_all?)` → 精确替换；**实现 Preview**：读现盘 + 内存应用 + 用 `go-udiff` 算 unified diff
  - 安全：拒绝绝对路径以外的非法字符；拒绝出当前 workspace 根（V1 严格）
- **Out of Scope**：bash、grep、job
- **验证**：
  - 单测全覆盖（tmp dir）
  - edit 错误情形（old 不存在、多匹配且未 replace_all）正确报错
  - **Preview 单测**：构造已知文件 + 已知 edit 参数 → 断言 Preview 返回的 unified diff 字符串与期望一致；Preview 不应改动磁盘上的文件

### I-17 grep / search

- **目标**：代码搜索
- **依赖**：I-15
- **In Scope**：
  - 优先尝试调外部 `rg`；不存在则降级到内置 `regexp` + 文件遍历
  - 返回 `path:line:match` 列表
- **Out of Scope**：sourcegraph 远程搜索（Crush 有的，不要）
- **验证**：单测构造 tmp 目录 + 已知匹配；外部 rg 缺失场景

### I-18 bash 工具（无权限审批）

- **目标**：能执行 shell 命令
- **依赖**：I-15
- **In Scope**：
  - `internal/tool/shell/bash`：用 `os/exec`，超时默认 120s
  - 输出截断（stdout/stderr 各 32KB 上限）
  - 退出码 + 时长返回给模型
- **Out of Scope**：权限审批（I-20）；交互式输入；后台进程（I-19）
- **验证**：单测 echo / ls / 故意失败命令 / 超时杀掉

### I-19 job 工具组（job_run / output / kill）

- **目标**：后台长进程管理
- **依赖**：I-18
- **In Scope**：
  - `job_run(cmd)` → `{job_id}`，后台 goroutine 读流到 ring buffer
  - `job_output(job_id, tail?)` → 拿最近输出
  - `job_kill(job_id)` → SIGTERM，2s 后 SIGKILL
  - 进程组管理（Setpgid），避免孤儿
- **Out of Scope**：跨重启恢复
- **验证**：单测起 `sleep 5` → 立刻 kill → exit code 检查；起 echo 循环 → 拿 tail

### I-20 Permission Manager + 执行模式 + approval agent + 全局规则持久化 + 黑名单

- **目标**：审批回调机制 + 3 种 execution mode + approval agent 命令审批 + 5 种 Decision + global 规则的磁盘持久化
- **依赖**：I-13 / I-15 / I-18
- **In Scope**：
  - `internal/execution/`：`Mode`（work / plan / auto）、mode 解析、mode policy、mode switch 事件 payload
  - `internal/permission/`：`Manager`、`Decision`（5 种：Allow / Deny / AlwaysCmd / AlwaysTool / AlwaysGlobal）、`Rule`
  - `internal/approval/`：approval agent 接口；输入为 command/cwd/risk/mode/context summary/rule match 信息，输出 allow/deny/unsure + reason
  - 两层规则存储：
    - **session 级**（AlwaysCmd / AlwaysTool）：内存 map
    - **global 级**（AlwaysGlobal）：序列化到 `~/.config/ub/permissions.yaml`，启动时加载
  - 黑名单正则（硬编码 `rm\s+-rf\s+/`、`mkfs\.`、`dd\s+.*of=/dev/`）：即便 always-rule match 也强制再问
  - `Manager.Ask(ctx, req)` 按 mode 调用注入的 human `Asker` 或 approval agent
  - 查询顺序：mode gate → 黑名单 → global rules → session rules → approval agent（auto only）→ human Asker
  - Asker 收到的请求包含可选的 `Preview`（来自 PreviewableTool）
- **Out of Scope**：TUI 弹窗实现（I-24）
- **关键签名**：
  ```go
  type Mode string // work / plan / auto
  type Asker interface {
      Ask(ctx context.Context, req Request) (Decision, error)
  }
  type ApprovalAgent interface {
      ReviewCommand(ctx context.Context, req Request) (ApprovalAgentDecision, error)
  }
  type Request struct {
      Tool    string
      Args    json.RawMessage
      Risk    Risk
      Mode    execution.Mode
      Preview *tool.Preview  // optional
  }
  // 持久化
  func LoadGlobalRules(path string) ([]Rule, error)
  func SaveGlobalRule(path string, r Rule) error  // append + atomic write
  ```
- **验证**：
  - 单测：mock Asker，跑 5 种 Decision 路径
  - 单测：`plan` 模式拒绝 write 风险工具且不触发 Execute
  - 单测：`work` 模式下未命中 allow-rule 的 exec 工具走 human Asker
  - 单测：`auto` 模式下 approval agent allow 时不问用户；deny/unsure/error 时回退 human Asker
  - 黑名单优先级测试（即便 global rule match 也再弹）
  - 持久化：写入 AlwaysGlobal → 重启加载 → 同样 call 不再问 Asker
  - 原子写：模拟写入中途 panic → permissions.yaml 不被破坏（用临时文件 + rename）

### I-21 Agent loop v1（含 tool use）

- **目标**：端到端，能跑 "read main.go 并报告里面的函数"
- **依赖**：I-07 ~ I-20
- **In Scope**：
  - `internal/agent/`：`Agent.Run(ctx, sess, userMsg) error`
  - 从 session/config/CLI 注入 `execution.Mode`；每轮 tool dispatch 都带当前 mode
  - 单 session 内顺序处理 turns，maxTurns=25
  - 把工具 schema 传给 provider；解析模型 tool_use；调 Registry
  - **dispatcher 两阶段调用**：若工具实现 `PreviewableTool` → 先 Preview → 把 Preview 喂 `permission.Manager.Ask(Request)` → Allow 时才 Execute
  - `plan` 模式下模型请求 write tool 时，dispatcher 返回 tool error 而不写盘
  - tool_result 回写消息流，进 rollout
  - 让 anthropic / openai provider 支持 tool calls（更新流式 Event 类型）
  - CLI 子命令变成 `ub run -p "..."`（headless，支持 `--mode`），保留 `ub chat` 作为"裸聊天不带工具"
- **Out of Scope**：TUI、permission UI（用 mock auto-allow asker）、并行 tool call、loop detection、auto summary
- **关键签名**：见 design §3
- **验证**：
  - **fake provider 单测**（无需任何外部依赖）：构造 fake script "调 fs.read → 用 result 文本回答" → assert 最终消息正确
  - **模式单测**：fake script 先调 fs.edit；`--mode plan` 下断言文件未改且模型收到 denied/tool error
  - vcr 集成测试：固定 prompt → 模型调 fs.read → tool 真实执行 → 模型给最终答复 → 断言含期望关键词
  - 冒烟：`ub run --dev -p "在当前目录下有几个 .md 文件？"` 走本地 vLLM 跑通

---

## Sprint 3 — TUI

### I-22 Bubble Tea 骨架

- **目标**：能跑起来一个空 chat UI
- **依赖**：I-01
- **In Scope**：
  - `internal/tui/`：根 model + 三个组件（输入框、消息列表、状态栏）
  - 启动 `ub` 不带子命令时进入 TUI
  - 输入回车回显到消息列表（暂不调 agent）
  - Ctrl+C 退出
- **Out of Scope**：流式渲染、agent 接入
- **验证**：手测打开后能输入、看到回显；`teatest` 单元测试模拟按键

### I-23 流式渲染与 Agent 接入

- **目标**：TUI 调真实 Agent，token 流到屏幕
- **依赖**：I-21 / I-22
- **In Scope**：
  - TUI 与 Agent 之间用 channel：UI 发 `UserSend`，Agent 推 `DeltaText`、`ToolCallStart`、`ToolCallEnd`、`Done`
  - 消息列表组件支持流式追加
  - 状态栏显示当前 model / execution mode / turn 序号
- **Out of Scope**：权限弹窗、diff 渲染、slash 命令
- **验证**：手测在 TUI 内问 "say hi"；按 Ctrl+C 可中断；`teatest` 模拟一段流（fake provider）

### I-24 权限弹窗（modal）

- **目标**：危险操作真的能问到人；5 个 Decision 选项全部可触达
- **依赖**：I-20 / I-23
- **In Scope**：
  - `internal/tui/dialog/permission`：modal 显示工具名、参数预览、风险等级
  - 若 `Request.Preview != nil`：在 modal 中嵌入 diff 摘要（一行 summary + 折叠 unified diff，按 `d` 展开）
  - Plan 模式下的 exec 审批必须显示 "Plan mode: command may still have side effects"
  - auto 模式下若 approval agent 拒绝 / 不确定 / 出错，modal 展示 approval agent reason 后要求用户显式决策
  - 5 个审批候选以列表展示，说明每个选项的作用范围；方向键选择，Enter 确认，数字键仅保留为快捷键：
    - Allow once
    - Deny
    - Always allow exact command (session)
    - Always allow tool (session)
    - Always allow tool (global, persist)
  - 选 `5` 时调 `permission.SaveGlobalRule`，并在 modal 上短暂提示 "saved to ~/.config/ub/permissions.yaml"
  - Permission Manager 的 Asker 切换为 TUI 实现
- **Out of Scope**：完整 diff 渲染（I-25 做带语法高亮的 diffview）
- **验证**：
  - 手测让模型跑 bash，弹框正常；auto 拒绝时能回退人工弹框
  - 手测让模型 edit 文件 → modal 里能看到 Preview 摘要
  - `teatest` 单测 5 个选项的按键流：选 `5` 后磁盘文件出现对应规则

### I-25 富 Diff 渲染组件

- **目标**：把 I-24 modal 里的 Preview 折叠区升级为带语法高亮的 diffview
- **依赖**：I-16 / I-24
- **In Scope**：
  - `internal/tui/diffview`：unified diff 渲染，按语言用 `chroma` 高亮
  - 多文件 diff：FileDiff 列表 → 顶部 file tab，左右 / 上下切换
  - 嵌入 I-24 modal：按 `d` 展开后渲染富 diff
- **Out of Scope**：split view 双栏对照、行间编辑
- **验证**：手测让模型改两个文件，diff 渲染正常；单测 chroma 高亮对常见语言 (go / py / ts) 不 panic

### I-26 Slash 命令

- **目标**：基础工作流 hotkey
- **依赖**：I-22 / I-14
- **In Scope**：`/model`、`/approval-model`、`/mode`、`/clear`、`/sessions`、`/help`、`/quit`、`/config`、`/profile`
- **补充要求**：`/approval-model [model]` 只切换 auto 模式使用的审批模型；无参数时展示候选列表，显式指定时校验候选模型，切换后重建 approval agent 并只影响后续命令审批
- **Out of Scope**：自定义 alias、命令补全
- **验证**：手测每个命令；单测命令解析

---

## Sprint 4 — 上下文管理

### I-27 Token 估算

- **目标**：发请求前知道大概用多少 token
- **依赖**：I-05
- **In Scope**：
  - `internal/context/`：`Estimate(msgs []Message, model string) int`
  - request 级估算包含 tool schema，并提供 system/runtime、tool schema、user/assistant、tool result 的轻量 breakdown
  - 用 `tiktoken-go`（OpenAI 系准）+ 简单字符近似（Claude / Ollama）
  - 响应里若有 `usage` 就回灌缓存校正，并保存 input/output/reasoning/cache read/cache write 中 provider 支持的字段
- **Out of Scope**：精确 BPE for Anthropic
- **验证**：单测：已知字符串 / 已知 token 数对照

### I-28 自动 Summary

- **目标**：长会话不爆 context
- **依赖**：I-21 / I-27
- **In Scope**：
  - Agent 发请求前检查 `(estimated tokens + reserve_output_tokens) / model.MaxContext > threshold`（默认 0.8）
  - 触发：用 small_model 跑 `summary` prompt 模板（embed template）
  - 替换早期消息为单条 anchored system 摘要；最近原文按 `keep_recent_turns` 和 token budget 保留，按完整 user turn 截断
  - tool result 默认限幅到 12KiB/400 行，完整输出作为 state artifact 保存；rollout 只保存模型可见 preview 与 truncation metadata
  - rollout 写一条 `Summary` 事件
  - TUI 状态栏展示 `ctx est` / `ctx last`；`/compact` 可主动触发同一压缩逻辑
- **Out of Scope**：provider 专属远程 compact API
- **验证**：单测：构造超长历史和大 tool result → 触发 → 历史被替换为 summary + 预算内最近 turn；rollout 多一条 Summary 事件；`/compact` 主动压缩并刷新状态栏 context 用量

---

## Sprint 5 — MCP & LSP

### I-29 MCP client（stdio）

- **目标**：能起一个 MCP server 并列出工具
- **依赖**：I-15
- **In Scope**：
  - `internal/mcp/`：stdio transport + JSON-RPC 2.0 frame
  - 实现 `initialize` / `tools/list` / `tools/call`
  - 用 `npx @modelcontextprotocol/server-filesystem` 做 e2e
- **Out of Scope**：http / sse 传输；resources / prompts
- **验证**：集成测试启子进程 → `tools/list` 拿到列表 → `tools/call` 读一个文件

### I-30 MCP http / sse 传输 + 注入 Registry

- **目标**：把 MCP 工具混进本地 Registry
- **依赖**：I-29
- **In Scope**：
  - 传输层补 http、sse 两种
  - `mcp.Tool` 实现本地 `Tool` 接口；名字加 `mcp__<server>__<tool>` 前缀
  - 启动时按 config 拉所有 server；某个失败不影响其它
- **Out of Scope**：resources / prompts 集成
- **验证**：起两个 server，能在 `ub run` 里看到两套工具

### I-31 LSP client 基础

- **目标**：能起 gopls，做 didOpen / didChange
- **依赖**：I-16
- **In Scope**：
  - `internal/lsp/`：JSON-RPC over stdio
  - lifecycle：initialize / initialized / didOpen / didChange / shutdown
  - 文件监听：edit/write 工具执行后主动通知 LSP
- **Out of Scope**：completion、hover
- **验证**：集成测试起 gopls，发 didOpen 一个 .go 文件，无错误

### I-32 LSP 工具：diagnostics / references

- **目标**：模型可以看代码错误、跳定义
- **依赖**：I-31
- **In Scope**：
  - `diagnostics(file?)` 工具
  - `references(symbol, path?)` 工具，兼容 `references(file, line, col)` 位置查询
  - 都先 didChange 同步本地修改再查询
- **Out of Scope**：rename、code action
- **验证**：故意写错语法的 .go 文件 → diagnostics 拿到错误；vcr 集成测试让模型用 references 跳转

---

## Sprint 6 — 收尾

### I-33 Session resume（CLI + TUI）

- **目标**：重启后能继续上次的会话
- **依赖**：I-09 / I-22
- **In Scope**：
  - `ub --resume`（拉最近一个 session）
  - `ub --resume=<id>` / `ub --resume <id>`
  - TUI 内 `/sessions` 选择或切换历史 session
  - TUI 启动时如果有最近 session 询问是否 resume
  - 恢复 session 时还原最近一次 `ModeSwitch`，否则使用当前 CLI/config mode
- **Out of Scope**：跨设备同步
- **验证**：开一个会话 → 退出 → resume → 历史完整出现

### I-34 `ub rollout show`

- **目标**：调试 / 审计可读
- **依赖**：I-09
- **In Scope**：
  - 子命令读所有事件 → 漂亮打印（lipgloss 着色）
  - `--json` 输出原始 JSONL
  - `--turns 5..10` 过滤
- **Out of Scope**：编辑事件、导出 markdown
- **验证**：固定 cassette 跑一遍 `ub run` → rollout show 输出包含期望段落

### I-35 README、安装文档、第一个 release

- **目标**：v0.1.0 tag
- **依赖**：全部
- **In Scope**：
  - README（中英双语段落）
  - `docs/install.md`
  - 启动期 best-effort 清理：session TTL + per-workspace 最近保留、日志轮转默认值与配置 schema
  - GoReleaser 配置（多平台二进制）
  - GitHub Actions 上 release workflow
- **Out of Scope**：homebrew tap、npm 包
- **验证**：`go test ./...` 通过；构造旧 session / 大日志文件后启动 `ub`，确认 session/events 清理和日志轮转；CI 通过 → push tag → release 产物可下载

---

## 跨迭代约定

### 提交规范
- 每个迭代独立提交，message 前缀 `[I-NN] <summary>`
- 该迭代的测试 / 文档变更也进同一 commit（除非显著扩大 diff）

### 测试约定
- 涉及外部 IO：必须 vcr 或 tmp dir
- 不写"无断言"的"测试"
- 集成测试与单测分开（`testing.Short()` 跳过慢的）

### 三层测试金字塔
- **快速单测**（默认 `go test ./...`）：fake provider + tmp dir，零外部依赖，全部用例 < 30s
- **集成测试**（`go test -tags=integration`）：vcr replay 真实历史请求，覆盖 provider 适配层
- **本地端到端**（手测 `ub run --dev ...`）：跑真实本地推理服务（vLLM/Ollama），验证 prompt 与模型表现

### 文档同步
- 每个迭代完成后：如果改了 API/行为，对应更新 `design.md`
- `roadmap.md` 自身在每迭代完成时 *勾掉*（保留历史；只有需求变化才改未来）

### 开放问题（暂留待迭代时拍）
- `module path`：`github.com/feimingxliu/ub`？域名也可（`feiming.dev/ub`）→ I-01 时定
- token 估算用 tiktoken-go 还是各家 SDK 自带计算 → I-27 决
- gopls 用法：embed protocol 自己实现 vs 复用 `go.lsp.dev/protocol` → I-31 调研
