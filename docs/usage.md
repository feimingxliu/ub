# ub 使用文档

本文档面向已经完成 [`install.md`](install.md) 安装与 Provider 配置的用户，覆盖 TUI 键位、Slash 命令、执行模式、权限审批、上下文管理、常见工作流与故障排查。

英文 README 见 [`../README.md`](../README.md)，中文 README 见 [`../README.zh-CN.md`](../README.zh-CN.md)。

## 1. CLI 入口速览

| 命令 | 用途 |
|---|---|
| `ub` | 启动 TUI（默认行为，无子命令时） |
| `ub --resume` | 列出当前工作区历史 session，选一个恢复 |
| `ub --resume=<id>` | 直接恢复指定 session |
| `ub --provider <name> --model <id>` | 启动 TUI 时临时覆盖 provider / model |
| `ub --mode work\|plan\|auto` | 启动时指定执行模式 |
| `ub --profile <name>` / `--dev` | 选择 profile（`--dev` 等价 `--profile dev`） |
| `ub run -p "..."` | 无头模式跑一次 agent loop（CI / 脚本） |
| `ub chat "..."` | 单轮直接对话，不带工具、不进 rollout（验证 provider 通路用） |
| `ub sessions ls` | 列出当前工作区的 session |
| `ub sessions rm <id>` | 删除指定 session（事件 CASCADE 一起删） |
| `ub sessions clear --yes` | 清空当前工作区所有 session |
| `ub sessions clear --all --yes` | 清空所有工作区的 session（跨 workspace） |
| `ub rollout show <id>` | 漂亮打印某个 session 的所有事件 |
| `ub rollout show <id> --json` | 输出原始 JSONL（机器可读） |
| `ub rollout show <id> --turns 5..10` | 只看第 5 到 10 轮 |
| `ub config show` | 打印合并后的有效配置 |
| `ub config path` | 列出本次加载用到的配置文件 |
| `ub doctor` / `ub doctor --plain` | 环境健康检查（plain 关闭颜色，CI 友好） |

> **会话隔离**：sessions 按 `cwd` 字符串严格隔离。在 `/proj` 和 `/proj/sub` 启动会被视作不同工作区，互相看不到对方的历史。

## 2. TUI 键位

### 2.1 通用

| 键 | 作用 |
|---|---|
| `Ctrl+C` | 退出 TUI |
| `Esc` | 关闭弹出层（文件选择 / 选择器） / 中断当前运行中的 agent |
| `Shift+Tab` | 切换执行模式（work → plan → auto → work） |
| `Enter` | 发送当前输入；输入为空且有 collapsible 块聚焦时，展开/收起聚焦项 |
| `Tab` | 触发补全：`/` 后补全 slash 命令、`@` 后补全工作区文件 |
| `PgUp` / `PgDn` | 消息区翻页 |

### 2.2 活动块（thinking / tool）展开导航

| 键 | 作用 |
|---|---|
| `Ctrl+O` | 展开最近一个 collapsible 块（两段式：先展开最近的工具组，再展开组内最近的工具项的彩色 diff） |
| `Ctrl+N` | 焦点移到下一个 collapsible 块 |
| `Ctrl+P` | 焦点移到上一个 collapsible 块 |
| `Enter`（输入为空） | 展开 / 收起当前焦点块 |

### 2.3 鼠标

- **滚轮**：消息区翻页
- **左键点击**：点击 collapsible 块的标题行，展开 / 收起该块
- 鼠标支持 cell-motion 模式；如果要拖拽选择复制，按住终端的「修饰键」（多数终端是 `Option`/`Alt` 或 `Shift`）后再拖。

### 2.4 输入区

| 输入前缀 | 行为 |
|---|---|
| 普通文本 | 发送给 agent，进入 rollout，可触发工具调用 |
| `/<command>` | 执行 slash 命令（见 §3） |
| `!<command>` | 本地直跑 shell，输出直接显示，**不走模型、不审批、不进 rollout** |
| `@<prefix>` | 触发工作区文件候选，选中后插入相对路径引用（agent 读到时知道指向哪个文件） |

输入排队：agent 运行中再输入新内容，按 `Enter` 会把它加入队列，agent 一轮结束后自动跑下一条。`Up` / `Down` 在队列里穿梭编辑。

### 2.5 权限弹窗

弹窗出现时方向键选择，`Enter` 确认；也可以直接按数字键：

| 数字键 | Decision | 作用范围 |
|---|---|---|
| `1` | Allow once | 仅本次 |
| `2` | Deny | 拒绝本次 |
| `3` | Always allow exact command | 本 session 内同样的命令免问 |
| `4` | Always allow tool | 本 session 内该工具全部免问 |
| `5` | Always allow tool (global) | 永久允许，写入 `~/.config/ub/permissions.yaml` |

弹窗中如果有 Preview（write/edit 工具有 diff 预览），按 `d` 展开 / 收起 diff 折叠区。

> **黑名单始终生效**：诸如 `rm -rf /`、`mkfs.*`、`dd of=/dev/*` 之类的命令，即使你之前选过 "always allow"，弹窗也会再次出现。

## 3. Slash 命令

| 命令 | 参数 | 作用 |
|---|---|---|
| `/help` | — | 列出所有 slash 命令 |
| `/quit` / `/exit` | — | 退出 TUI |
| `/config` | — | 展示当前 model / mode / cwd |
| `/clear` | — | 清空当前对话视图（不删 session） |
| `/new` | — | 在当前工作区新建一个空 session（旧的保留） |
| `/sessions` | — | 列出当前工作区的历史 session，方向键选择 + Enter 切换 |
| `/provider` | `[provider] [model]` | 无参数列可用 provider；带参数切换 provider（可同时换 model） |
| `/model` | `[model]` | 无参数列当前 provider 下可用 model；带参数切换 |
| `/effort` | `[level]` | 切换 reasoning effort（`none` / `minimal` / `low` / `medium` / `high` / `xhigh`），仅对支持 reasoning 的模型生效 |
| `/approval-model` | `[model]` | 设置 auto 模式下用作审批 agent 的模型，无参数列候选；不影响主对话 model |
| `/mode` | `<work\|plan\|auto>` | 切换执行模式（也可按 `Shift+Tab` 循环切） |
| `/compact` | — | 主动触发上下文压缩（用 `small_model` 生成摘要） |
| `/profile` | `<name>` | 显示切换 profile 的提示（需要重启 ub 才生效，因为 profile 影响启动期加载） |

## 4. 执行模式

| 模式 | 文件写入 | 命令执行 | 适用场景 |
|---|---|---|---|
| **`work`**（默认） | ✅ 允许 | 需审批 | 日常编码：让 agent 真改文件 + 跑命令 |
| **`plan`** | ❌ 拦截（dispatcher 直接返回 tool error） | 需审批 | 探索 / 规划：先让 agent 调研，确认后再切回 work |
| **`auto`** | ✅ 允许 | 由审批 agent 自动审批，不确定时回退人工 | 受信工作流 / 长任务无人值守；需要先在配置里启用 `approval_agent` |

切换模式立刻生效，并在 rollout 中写一条 `ModeSwitch` 事件。

`auto` 模式启用步骤：

```yaml
approval_agent:
  enabled: true
  model: gpt-4o-mini       # 或别的便宜模型；不必和主对话同 provider
  reasoning:
    effort: low
```

切换到 auto 后，agent 请求执行命令时会先把 `command / cwd / risk / mode / context summary / rule match` 喂给审批模型，模型返回 `allow / deny / unsure + reason`。`allow` 时不再问用户；`deny` / `unsure` / 出错时回退人工弹窗，并把审批模型的 reason 展示出来。

## 5. 权限系统

### 5.1 查询顺序

1. **模式闸门**：plan 模式直接拦截写工具
2. **黑名单**：硬编码正则匹配（`rm -rf /`、`mkfs.*`、`dd of=/dev/*`），即便有 always-rule 也强制再问
3. **全局规则**：`~/.config/ub/permissions.yaml`，由用户在弹窗选 "Always allow (global)" 时自动写入
4. **Session 规则**：内存中，AlwaysCmd / AlwaysTool 选项的范围
5. **审批 agent**（仅 auto 模式）：模型 allow 时通过；否则回退第 6 步
6. **人工弹窗**：方向键选 + Enter

### 5.2 全局规则文件示例

```yaml
# ~/.config/ub/permissions.yaml — ub 自己维护，可手动编辑
rules:
  - tool: bash
    command_pattern: ^ls( .*)?$
    decision: allow
    source: user
  - tool: read
    decision: allow
    source: user
```

`command_pattern` 是 Go 的 `regexp` 语法；删除某条规则只需要把它从 YAML 里去掉，下次启动加载即可。

### 5.3 风险等级

工具实现声明 `Risk()`，决定权限询问粒度：

- `safe`：默认不问（read / ls / glob / grep / LSP 工具）
- `write`：默认问（write / edit）
- `exec`：默认问（bash / job_run / job_kill）
- `network`：默认问（如果某些 MCP 工具声明）

## 6. 上下文管理

### 6.1 自动压缩（auto summary）

每次发请求前，agent 会估算 `(input tokens + reserve_output_tokens) / model.max_context`。超过阈值（默认 0.8）触发：

1. 用 `small_model` 跑摘要 prompt 模板，把早期消息总结成一条 system 摘要
2. 保留最近 `keep_recent_turns` 个完整 user turn（按 token budget 截断，但按 user turn 边界对齐）
3. tool result 默认限幅 12 KiB / 400 行，完整输出落到 `$XDG_STATE_HOME/ub/tool-output/`，rollout 里只存 preview + truncation metadata
4. rollout 写一条 `Summary` 事件

### 6.2 手动压缩

TUI 内 `/compact` 立即触发同一压缩逻辑，状态栏会显示 `Compacting...`。

### 6.3 状态栏 context 读数

状态栏会显示：

- `ctx est`：下次请求估算 token 用量 / 当前模型 max_context
- `ctx last`：上次实际响应里 provider 报告的 usage（如果 provider 返回了）

读数变红时说明接近阈值，可以主动 `/compact` 或者 `Shift+Tab` 切到 plan 模式探索完再切回来。

### 6.4 关键配置

```yaml
context:
  reserve_output_tokens: 4096       # 给输出预留的 token
  tool_results:
    max_chars: 30000                # tool result 写入 message history 前的硬限幅
    spillover_max_age: 24h          # 溢出文件保留时长
```

## 7. 常见工作流

### 7.1 让 ub 改代码并跑测试

```
> 请把 internal/foo/bar.go 中的 ProcessBatch 函数改成并发处理，最多 4 个 goroutine。改完跑 go test ./internal/foo/...
```

agent 会：
1. `read internal/foo/bar.go` 看现状
2. `edit` 改文件（弹窗预览 diff，按 1 / Enter 允许）
3. `bash go test ./internal/foo/...` 跑测试（弹窗审批）
4. 失败时自己分析输出再改一轮

### 7.2 plan 模式探索仓库

```
ub --mode plan
> 这个项目的 agent loop 是怎么处理多 turn tool use 的？画一个流程图（不要改代码）
```

agent 只能用 read / search / LSP 工具，不能改文件、不能跑命令（命令工具会被弹窗拦截）。研究完想动手时按 `Shift+Tab` 切到 `work`。

### 7.3 Headless / CI 跑批

```sh
ub run -p "把 docs/*.md 里所有过期的 API 引用更新成新的命名" --mode work
```

- 没有交互；任何需要审批的工具调用会失败并打印到 stderr。CI 场景建议提前在 `~/.config/ub/permissions.yaml` 加好白名单，或者配置 `approval_agent` + `--mode auto`。
- 退出码：0 成功；非 0 表示 agent 遇到错误或被拦截。

### 7.4 Resume 上次会话

```sh
ub --resume                  # 弹列表选
ub --resume=abc123           # 直接切到 session abc123
```

Resume 时会恢复最后一次 `ModeSwitch` 设置的 mode；如果该 session 从未切过 mode，使用当前 CLI/config 默认。

### 7.5 调试某次会话

```sh
ub rollout show abc123 | less          # 彩色 pretty-print
ub rollout show abc123 --json | jq .    # 机器可读
ub rollout show abc123 --turns 3..5     # 只看第 3-5 轮
```

事件类型：`UserMessage` / `AssistantMessage` / `ToolCall` / `ToolResult` / `Summary` / `ModeSwitch` / `PermissionDecision` / `Error`。

## 8. Profiles（开发期切配）

`profiles:` 节允许声明一组覆盖项，命令行 `--profile <name>` / `--dev`（= `--profile dev`） / 环境变量 `UB_PROFILE` 任选其一启用。

典型场景：开发时切到本地 vLLM 跑测试，不动主配置。

```yaml
default_provider: openai
default_model: gpt-4o-mini
providers:
  openai:
    type: openai
    api_key: ${OPENAI_API_KEY}
  local:
    type: openai-compat
    base_url: http://127.0.0.1:8000/v1

profiles:
  dev:
    default_provider: local
    default_model: openai/gpt-oss-20b
    execution_mode: plan
```

```sh
ub --dev                # 等价 --profile dev，切到本地 vLLM + plan 模式
UB_PROFILE=dev ub       # 同上
```

## 9. MCP 与 LSP

### 9.1 MCP server 接入

```yaml
mcp_servers:
  filesystem:
    transport: stdio
    command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/path/to/expose"]
  remote-api:
    transport: http
    url: https://example.com/mcp
  events:
    transport: sse
    url: https://example.com/mcp/events
```

启动时 ub 会自动 `initialize` + `tools/list`，把工具名加 `mcp__<server>__<tool>` 前缀注册到主工具表。某个 server 失败不影响其它（fail-open + 日志记录）。MCP 工具调用走相同的权限审批流程。

### 9.2 LSP 接入

```yaml
lsp_servers:
  go:
    command: ["gopls"]
    file_patterns: ["*.go"]
  ts:
    command: ["typescript-language-server", "--stdio"]
    file_patterns: ["*.ts", "*.tsx", "*.js", "*.jsx"]
```

agent 可以用：

- `diagnostics(file?)`：查当前文件（或整个 workspace）的错误 / 警告
- `references(symbol, path?)` 或 `references(file, line, col)`：找符号引用

ub 在 write / edit 工具执行后会主动给 LSP 发 `didChange`，保证 diagnostics 反映最新内容。

## 10. Plan-then-execute

ub 提供两个 plan 工具,工作流是:**plan 模式产出 artifact → work 模式照单施工 → 每完成一步打勾**。

1. **进 plan 模式**(`Shift+Tab` 循环模式,或 `--mode plan` 启动),让 agent 把思路 `plan_write` 成一个 markdown:

   ```
   plan_write(
     title="Migrate auth middleware",
     steps=["read existing middleware","grep call sites","write new middleware","update tests","run go test"],
     notes="see issue #128 for compliance requirements"
   )
   ```

   工具会在 `<workspace>/.ub/plans/<时间戳>-<slug>.md` 写一个文件,并把 `plan_id` 返回。你可以直接打开 review、改步骤(只要保留 `- [ ] N. <text>` 行的格式)。

2. **切到 work 模式**继续会话。agent 用 `read` 看 plan,按顺序执行;每完成一步调 `plan_update_step`:

   ```
   plan_update_step(plan_id="20260527T..", step_index=2, status="done", note="found 12 call sites")
   ```

   状态机:`done`(`[x]`)/ `skipped`(`[~]`)/ `failed`(`[!]`)/ `pending` 回退(`[ ]`)。当所有步骤都 ≠ `[ ]` 时,文件顶部的 `Status:` 自动变 `complete`,并在 `## Log` 末尾追加一条带时间戳的记录。

3. **中断恢复**:下次会话直接 `read .ub/plans/<id>.md`(或让 agent 主动 `ls .ub/plans/` 列出未完成的)。.ub/plans/ 是工作区文件,可以 commit 也可以 `.gitignore`,自己决定。

## 11. Workspace 持久记忆

ub 维护两个 markdown 文件,在每次 agent 开跑时按 budget 注入到 system prompt:

- `<workspace>/.ub/memory.md` —— 跟着项目走,适合"build 命令是 X、issue #42 修在 auth.go:120"等团队事实
- `~/.config/ub/memory.md` —— 跟着用户走,适合"我偏好 pnpm、VPN URL 是 Z"等个人习惯

写入方式:`remember` 工具 —— `remember(text="...", scope="workspace" 或 "global")`。新条目以 `## <RFC3339 时间>` 标题追加到对应文件末尾,可以手改可以 git commit。

注入预算:`config.yaml` 的 `memory.max_chars`(默认 4000)。超过预算时**保留最新的尾部条目**,头部加 `... [memory truncated]` 提示。

```yaml
memory:
  max_chars: 8000
```

本版本只交付显式写入路径。LLM 自动归纳(每轮判断是否值得 remember)留待后续版本。

## 12. 长输出落盘(spillover)

ub 会把每次 tool result 在 inline 限额内的预览 + 完整内容落盘到磁盘。磁盘路径默认是 `<XDG_STATE_HOME 或 ~/.local/state>/ub/tool_outputs/<sessionID>/<toolUseID>.txt`,模型在 result 的 footer 里会拿到 `full_output_path=<绝对路径>` 提示,可以用 `read` 工具或新引入的 `tool_result(tool_use_id)` 工具拉回。

相关配置(`config.yaml` 的 `context.tool_results`):

```yaml
context:
  tool_results:
    inline_max_bytes: 12288        # 默认 12KB
    inline_max_lines: 400          # 默认 400
    full_max_bytes: 52428800       # 默认 50MB,spillover 文件上限
    spillover_dir: /var/tmp/ub-out # 默认走 XDG_STATE_HOME;改盘时设这里
    spillover_max_age: 168h        # ub doctor / startup cleanup 用
```

bash / job_output 的 result content 顶部有一个稳定的 `<shell_metadata>` 块,字段:

```
<shell_metadata>
exit_code=0
duration_ms=42
timeout=true        # 仅在超时 kill 时出现
aborted=true        # 仅在 ctx 取消 kill 时出现
error=<msg>         # 启动失败或其他错误
</shell_metadata>
```

## 13. Hooks(生命周期 shell 钩子)

在 `~/.config/ub/config.yaml` 或项目 `.ub/config.yaml` 的 `hooks` 段挂载 shell 命令,在 agent 关键节点触发:

```yaml
hooks:
  pre_tool_call:
    - command: ["./scripts/audit.sh"]   # argv,非 shell 字符串
      tools: ["bash"]                    # 空 = 所有 tool
      timeout: 5s                        # 默认 10s,最大 60s
      on_failure: warn                   # warn / block;block 只在 pre_tool_call 生效
      env: ["HOME", "PATH"]              # env 白名单;默认仅 PATH
  post_tool_call:
    - command: ["gofmt", "-w", "."]
      tools: ["edit", "write", "multiedit"]
  pre_user_turn:
    - command: ["./scripts/snapshot-wip.sh"]
  post_user_turn:
    - command: ["./scripts/notify-done.sh"]
```

子进程能拿到:

- **stdin**:JSON,字段 `event` / `session_id` / `turn`,tool 阶段还有 `tool.{name,use_id,args}`,post_tool_call 还有 `result.{content,is_error}`
- **env**:`UB_HOOK_EVENT` / `UB_HOOK_SESSION_ID` / `UB_HOOK_TURN`(以及 tool 阶段的 `UB_HOOK_TOOL_NAME` / `UB_HOOK_TOOL_USE_ID`),加上配置 `env` 白名单里列出来的父进程变量

行为约定:

- `pre_tool_call` 配 `on_failure: block` 且 hook exit ≠ 0 时,工具 **不会执行**,模型看到一个带 hook stderr 的 IsError tool_result
- 其他三个触发点(`post_tool_call` / `pre_user_turn` / `post_user_turn`)的 `block` 设定会被忽略;hook 失败只通过 TUI activity 流提示,不会改 tool result 也不阻断 user turn
- timeout 到达时子进程会被 SIGKILL,outcome.Err 含 `timeout` 字样
- stdout 与 stderr 各自截断到 4KB,剩余直接丢弃

工作区(`.ub/config.yaml`)与用户(`~/.config/ub/config.yaml`)的 hook 列表是 **追加合并**,不是覆盖 —— 工作区 hook 接在用户 hook 后面跑。

## 14. 故障排查

| 症状 | 原因 / 解决 |
|---|---|
| `provider "xxx" not configured` | `ub config show` 看合并后的 providers 是否包含 xxx；`default_provider` 必须与某个 provider key 匹配 |
| 401 Unauthorized | 环境变量未注入；`echo $OPENAI_API_KEY` 看是否为空；YAML 里写 `${OPENAI_API_KEY}` 而不是硬编码 key 字面值 |
| 本地 vLLM / Ollama 连不上 | `ub doctor --plain` 看探测结果；`curl <base_url>/models`（OpenAI 兼容；Ollama 用 `/v1/models`） |
| 模型不支持 tool calling | `ub doctor` 会标注；本地小模型常见，换支持 tool calling 的模型，或改用 `ub chat` 单轮模式 |
| TUI 内 Enter 没反应 | 检查是不是停在中文输入法候选状态；权限弹窗里需要先方向键选 / 直接按数字键 |
| 鼠标点击不响应 / 块没法展开 | 确认没有 modal / 选择器开着（按 `Esc` 关掉）；如终端不支持鼠标，用 `Ctrl+O` / `Ctrl+N` / `Ctrl+P` + `Enter` 替代 |
| 切到 plan 模式后 write 工具报错 | 这是预期：plan 模式拦截写工具；切回 `work` 或按 `Shift+Tab` 循环 |
| 长会话越来越慢 | 主动 `/compact`；调小 `context.tool_results.max_chars`；切到更长 context 的模型 |
| 在 `/proj/sub` 看不到 `/proj` 的 session | sessions 按 cwd 字符串隔离；`cd` 到原 cwd 再 `ub --resume` |
| 想看完整 prompt | `UB_LOG_LEVEL=debug UB_LOG_FILE=/tmp/ub.log ub ...`，日志里有 provider 请求 |
| Agent 卡在某轮不出来 | `Esc` 中断当前轮；如果是 plan 模式下试图写文件，先看 tool error 信息再 retry |
| rollout 数据库被锁 | SQLite WAL 模式下少见，多半是另一个 ub 进程没退干净；`ps -ef | grep ub` 检查 |
| 命令未被审批就直接被拒 | 黑名单命中（`rm -rf /` 类）；这是有意拦截，参数稍微改一下（如显式列举目录）就能正常进入审批 |

更多上下文（架构 / 设计决策）参见 [`design.md`](design.md)；产品边界参见 [`requirements.md`](requirements.md)。
