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
| `ub --mode work\|plan\|auto\|full-access` | 启动时指定执行模式 |
| `ub --profile <name>` / `--dev` | 选择 profile（`--dev` 等价 `--profile dev`） |
| `ub run -p "..."` | 无头模式跑一次 agent loop（CI / 脚本） |
| `ub goal -p "..."` | 无头 goal mode：把 prompt 当作目标自动续跑，直到完成、阻塞或预算耗尽 |
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
| `ub prompt inspect` / `ub prompt inspect --json` | 检查当前 workspace 的 prompt section、来源、状态、大小与 token 估算；默认不显示正文 |
| `ub doctor` / `ub doctor --plain` | 环境健康检查（plain 关闭颜色，CI 友好） |

> **会话隔离**：sessions 按 `cwd` 字符串严格隔离。在 `/proj` 和 `/proj/sub` 启动会被视作不同工作区，互相看不到对方的历史。

## 2. TUI 键位

### 2.1 通用

| 键 | 作用 |
|---|---|
| `Ctrl+C` | 退出 TUI |
| `Esc` | 关闭弹出层（文件选择 / 选择器） / 中断当前运行中的 agent |
| `Shift+Tab` | 切换执行模式（work → plan → auto → full-access → work） |
| `Enter` | 发送当前输入；输入为空且有 collapsible 块聚焦时，展开/收起聚焦项。**运行中**按 Enter 把输入作为补充 prompt 注入当前回合（引导模型，见 §2.4） |
| `Ctrl+J` | 输入框内换行（多行输入），所有终端通用。输入框按内容自动增高，上限约为终端高度的 1/3，超出后内部滚动，不会挤占消息区 |
| `Shift+Enter` | 换行（同 Ctrl+J），但仅终端支持 Kitty 键盘协议时才生效（WezTerm / Ghostty / Kitty / 新版 iTerm2 直接运行时）。通过 SSH、tmux/screen 或老终端时 Shift+Enter 与 Enter 无法区分，请用 `Ctrl+J` |
| `Up` / `Down` | 智能切换：补全选择器打开时移动补全光标；否则光标在首行/末行时翻阅 `↑` 历史或排队队列，在中间行时在多行文本内上下移动光标（单行输入下与历史导航行为一致） |
| `Tab` | 触发补全：`/` 后补全 slash 命令、`@` 后补全工作区文件。**运行中**按 Tab 把输入排入下一回合队列（见 §2.4） |
| `PgUp` / `PgDn` | 消息区翻页 |
| `Ctrl+Home` / `Ctrl+End` | 跳到消息区最前 / 最后 |

### 2.2 活动块（thinking / tool）展开导航

| 键 | 作用 |
|---|---|
| `Ctrl+O` | 展开最近一个 collapsible 块（两段式：先展开最近的工具组，再展开组内最近的工具项；写类工具显示彩色 diff，其他工具显示普通文本详情） |
| `Ctrl+N` | 焦点移到下一个 collapsible 块 |
| `Ctrl+P` | 焦点移到上一个 collapsible 块 |
| `Enter`（输入为空） | 展开 / 收起当前焦点块 |

展开后的工具详情如果被 activity 层二次限幅，会出现 `activity detail truncated` 提示；当完整 tool result 已落盘时，详情会保留 `full_output_path=...` footer，便于用 `read` 或 `tool_result(tool_use_id)` 继续查看。如果只是当前 TUI 视窗装不下展开详情，消息区底部会显示 `[tool detail clipped: ...]`，可用 `PgUp` / `PgDn` 或滚轮继续查看。

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

> **多行输入**：输入框是 textarea，`Ctrl+J` 换行写多行 prompt（`Enter` 仍是发送，所有终端通用）；终端支持 Kitty 键盘协议时 `Shift+Enter` 也能换行，但通过 SSH、tmux/screen 或老终端时不可用，统一用 `Ctrl+J`。`@` 文件提及在光标所在行内匹配并插入；多行 prompt 作为整体 user message 发送，agent 会读到完整的多行内容。`/` slash 与 `!` shell 仍以整体输入首行前缀判断，因此多行时只在首行写命令、后续行作参数。`Up` / `Down` 在首行/末行翻阅历史或队列时会**整体替换**多行输入（原草稿在 `Down` 回到末尾时可恢复），在中间行则只在多行文本内移动光标。

#### 2.4.1 运行中输入：引导 vs 排队

agent 运行中再输入内容时，发送方式决定它的去向：

- **`Tab` = 排队下一回合**：把输入加入本地 FIFO 队列，agent 当前回合结束后自动跑下一条。排队消息在真正启动前不写入 rollout，可随时编辑。`Up` / `Down` 在队列里穿梭编辑。底部显示 `queued: N · next (TAB)`。
- **`Enter` = 引导当前回合（inject）**：把输入作为补充 prompt 注入正在运行的 agent loop，不另起回合——适合「之前没说清楚、模型已经在跑了，现在补充内容」。inject 与初始 prompt 共用同一个回合编号，靠时间戳排在正确位置；agent 在下一步执行后会读到它并据此调整。inject 不进 `↑` 历史。运行中底部提示 `Enter = guide this turn · TAB = queue for next turn`（有排队消息时该提示让位给队列视图）。

> 二者互斥：引导用于「修正/补充正在做的事」，排队用于「接下来还要做另一件事」。`/btw [question]` 是另一条独立路径，不排队也不引导，见 §3。

### 2.5 权限弹窗

弹窗出现时方向键选择，`Enter` 确认；也可以直接按数字键：

| 数字键 | Decision | 作用范围 |
|---|---|---|
| `1` | Allow once | 仅本次 |
| `2` | Deny | 拒绝本次 |
| `3` | Always allow exact command | 本 session 内同样的命令免问 |
| `4` | Always allow tool | 本 session 内该工具全部免问 |
| `5` | Always allow exact command (project) | 当前项目后续 session 内同样的命令免问，写入 `<workspace>/.ub/permissions.yaml` |
| `6` | Always allow similar command (project) | 当前项目后续 session 内相似命令免问，写入 Claude-style `Bash(cmd:*)` 规则 |

弹窗中如果有 Preview（write/edit 工具有 diff 预览），按 `d` 展开 / 收起 diff 折叠区。

> **黑名单始终生效**：诸如 `rm -rf /`、`mkfs.*`、`dd of=/dev/*` 之类的命令，即使你之前选过 "always allow"，弹窗也会再次出现。

## 3. Slash 命令

| 命令 | 参数 | 作用 |
|---|---|---|
| `/help` | — | 列出所有 slash 命令 |
| `/quit` / `/exit` | — | 退出 TUI |
| `/config` | — | 展示当前 provider / model / approval_model / small_model / mode / cwd |
| `/clear` | — | 清空当前对话视图（不删 session） |
| `/new` | — | 在当前工作区新建一个空 session（旧的保留） |
| `/sessions` | `[session-id\|search <query>]` | 列出当前工作区的历史 session，方向键选择 + Enter 切换；带 id 时直接切换 |
| `/resume` | `[session-id]` | 恢复历史 session：无参数打开选择器，带 id 时直接恢复 |
| `/rewind` | `[turn]` | 打开历史 user turn 选择器，回到所选消息之前，并把该消息放回输入框；若后续有文件改动，可选择只回退对话或同时尝试回退文件 |
| `/init` | `[guidance]` | 启动一轮 agent 调研当前工作区，并创建或改进 `AGENTS.md` |
| `/plans` | `[plan-id]` | 列出当前工作区的 plan artifact，方向键选择 + Enter 编辑；带 id 时直接打开 |
| `/plan-edit` | `<plan-id>` | 用 `$VISUAL` / `$EDITOR` 打开 state-root 下的 plan markdown，编辑后回到 TUI |
| `/provider` | `[provider] [model]` | 无参数列可用 provider；带参数切换 provider（可同时换 model） |
| `/model` | `[model]` | 无参数列当前 provider 下可用 model；带参数切换 |
| `/effort` | `[level]` | 切换 reasoning effort（`none` / `minimal` / `low` / `medium` / `high` / `xhigh`），仅对支持 reasoning 的模型生效 |
| `/approval-model` | `[model]` | 设置 auto 模式下用作审批 agent 的模型，无参数列候选；不影响主对话 model |
| `/small-model` | `[model]` | 设置 auto memory 使用的当前 provider 模型，无参数列候选；不影响主对话 model 或 compact summary |
| `/mode` | `<work\|plan\|auto\|full-access>` | 切换执行模式（也可按 `Shift+Tab` 循环切） |
| `/compact` | — | 主动触发上下文压缩（默认用当前主模型生成摘要） |
| `/btw` | `[question]` | 打开独立 BTW 视图；带问题时立即旁路询问，不排队、不打断当前 turn、不写入主历史。回答按普通助手消息 Markdown 渲染；视图内直接输入追问并按 `Enter` 继续，底部显示 BTW 专属状态行（`answering` 表示模型回答中，`idle` 表示可继续追问），`PgUp`/`PgDown` 或滚轮只滚动 BTW 输出，`Esc` 返回主对话并清空 BTW 历史，`Ctrl+Y` 复制最新答案，`Ctrl+U` 清空当前记录并留在 BTW |
| `/goal` | `[objective\|clear]` | 无参数展示当前 goal；带 objective 创建 active goal 并启动一轮 goal-oriented agent；`clear` 清除当前 session goal |
| `/profile` | `<name>` | 显示切换 profile 的提示（需要重启 ub 才生效，因为 profile 影响启动期加载） |

## 4. 执行模式

| 模式 | 文件写入 | 命令执行 | 适用场景 |
|---|---|---|---|
| **`work`**（默认） | ✅ 允许 | 需审批 | 日常编码：让 agent 真改文件 + 跑命令 |
| **`plan`** | ❌ 拦截（dispatcher 直接返回 tool error） | ❌ 拦截 | 探索 / 规划：先让 agent 调研，确认后再切回 work |
| **`auto`** | ✅ 允许 | 由审批 agent 自动审批，不确定时回退人工 | 受信工作流 / 长任务无人值守；需要先在配置里启用 `approval_agent` |
| **`full-access`** | ✅ 允许 | 默认放行，仍遵守黑名单 / deny / ask 规则 | 高信任本地批量修复：跳过常规审批但保留审计 |

切换模式立刻生效，只影响当前进程和后续请求；mode 不随 session 持久化。当前切换到 `full-access` 不会额外弹首次高风险确认 dialog；请把 CLI/config/TUI 的显式切换视为本次进程内授权。

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

1. **模式闸门**：plan 模式直接拦截写工具和执行命令
2. **黑名单**：硬编码正则匹配（`rm -rf /`、`mkfs.*`、`dd of=/dev/*`），即便有 always-rule 也强制再问
3. **项目 deny 规则**：`<workspace>/.ub/permissions.yaml` 中 `permissions.deny`
4. **项目 allow 规则**：`permissions.allow`
5. **Session 规则**：内存中，AlwaysCmd / AlwaysTool 选项的范围
6. **项目 ask 规则**：`permissions.ask`，命中后强制人工弹窗，即使在 auto 模式也不交给 approval agent
7. **full-access mode**：直接放行并记录 `allowed by full-access mode`
8. **审批 agent**（仅 auto 模式）：模型 allow 时通过；否则回退第 9 步
9. **人工弹窗**：方向键选 + Enter

### 5.2 权限规则文件示例

```yaml
# <workspace>/.ub/permissions.yaml — ub 自己维护，可手动编辑
permissions:
  allow:
    - Bash(go test ./...)
    - Bash(go test:*)
    - Bash(make check)
  ask:
    - Bash(git push:*)
  deny:
    - Bash(curl:*)
    - Bash(rm -rf:*)
```

项目权限文件通常属于本地信任选择，类似 Claude Code 的项目本地 settings；如果仓库没有忽略 `.ub/permissions.yaml`，建议把它加入 `.gitignore`。

规则语法参考 Claude Code：`Tool(pattern)`。`Bash(go test ./...)` 是精确命令；`Bash(go test:*)` 是 prefix/wildcard 规则，匹配 `go test` 和 `go test ./internal/...`。ub 会拆分 `&&`、`;`、管道和换行等 compound command；`Bash(git status:*)` 不会单独放行 `git status && rm -rf ./build`，除非每个子命令都命中 allow 规则。删除某条规则只需要把它从 YAML 里去掉，下次启动加载即可。

### 5.3 风险等级

工具实现声明 `Risk()`，决定权限询问粒度：

- `safe`：默认不问（read / ls / glob / grep / ask / plan 工具 / LSP 工具）
- `write`：默认问（write / edit / multiedit / apply_patch）
- `exec`：默认问（bash / job_run / job_kill）
- `network`：默认问（web_search / web_fetch,以及声明为联网风险的 MCP 工具）

## 6. 上下文管理

### 6.1 自动压缩（auto summary）

每次发请求前，agent 会估算 `(input tokens + reserve_output_tokens) / model.max_context`。超过阈值（默认 0.8）触发：

`model.max_context` 由 ContextWindowResolver 统一决定：显式配置的 `providers.<name>.models.<model>.max_context_tokens` 最高优先；否则使用当前模型/provider 能力，并结合相同 provider endpoint/model 的历史成功 usage 和 context overflow 修正。解析结果带 source/confidence，供 runtime event 和后续 ContextDecision 使用。

1. 用当前主对话模型跑摘要 prompt 模板
2. 保留最近 `keep_recent_turns` 个完整 user turn（按 token budget 截断，但按 user turn 边界对齐）
3. tool result 默认限幅 12 KiB / 400 行，完整输出落到 `$XDG_STATE_HOME/ub/tool-output/`，rollout 里只存 preview + truncation metadata
4. rollout 写一条 `Summary` 事件

压缩不会删除或隐藏 session 里的原始对话消息。恢复 session 时，TUI 仍按完整 transcript 展示；只是下一次发送给 provider 的请求上下文会从最近的 summary + 保留原文窗口开始。

如果本地估算没有提前触发，但 provider 返回可识别的上下文超限错误，agent 会强制执行一次同样的压缩流程并重试同一轮请求。重试后仍超限或仍失败时，ub 返回 provider 的原始错误，避免在失败循环里反复摘要。

窗口观察缓存在 `$XDG_STATE_HOME/ub/context-windows/`（未设置 XDG 时为 `~/.local/state/ub/context-windows/`）。缓存按 provider、清理敏感部分后的 base URL 和完整 model ID 隔离，只保存 token 数；可以安全删除。缓存损坏或不可写时 ub 会记录 warning 并回退到静态窗口，不影响正常请求。

压缩时不会把全部待压缩历史无条件一次性丢给 summary 模型。若 summary prompt 本身超过 summary 模型预算，agent 会按完整 user turn 分块摘要，再合并这些块摘要。单个 user turn 自身超预算时，ub 会返回明确错误，而不是按 message 或字符切碎语义。

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

### 6.5 Prompt harness

每次 agent 请求都会额外注入一组不写入 rollout history 的 system context:

- coding-agent 行为原则:先读文件再改、优先专用工具、失败后先诊断、只汇报真实验证状态
- workspace instructions:工作区根目录的 `AGENTS.md`
- git snapshot:启动时的 branch / default branch / `git status --short` / 最近提交,并明确标注为非实时快照
- durable memory:全局指令与项目自动记忆,见 §14

这些内容通过固定顺序的 prompt section registry 组装：`coding_agent`、`runtime`、`workspace_instructions`、`git_snapshot`、`execution_mode`、`memory`。只有状态为 `included` 的 section 会进入 provider 请求；被配置关闭、当前无内容或不适用于 no-tool 请求的 section 分别显示为 `disabled`、`unavailable`、`omitted`。

可以在不调用 provider、不创建 session 的情况下检查当前有效 prompt：

```sh
ub prompt inspect                         # 文本 manifest，默认不显示正文
ub prompt inspect --json                  # 机器可读 manifest
ub --mode plan prompt inspect             # 检查 plan mode section
ub prompt inspect --model gpt-5.2         # 指定 token 估算使用的模型
ub prompt inspect --show-content           # 显式展示正文，可能包含项目指令或 memory
```

字符数和 token 数用于诊断，不代表 provider 的精确计费。默认输出不会包含 `AGENTS.md` 或 memory 正文；只有显式使用 `--show-content` 才会展示。

相关配置:

```yaml
prompt:
  workspace_instructions:
    enabled: true
    max_chars: 12000
  git_snapshot:
    enabled: true
    max_chars: 4000
  compact_style: structured   # short / structured
```

## 7. 常见工作流

### 7.1 让 ub 改代码并跑测试

```
> 请把 internal/foo/bar.go 中的 ProcessBatch 函数改成并发处理，最多 4 个 goroutine。改完跑 go test ./internal/foo/...
```

agent 会：
1. `read internal/foo/bar.go` 看现状
2. `apply_patch` 改文件（多行、结构性或跨文件改动优先使用 `*** Begin Patch` 信封，在 `@@` hunk 中保留足够的未改动上下文；位置不唯一时工具会拒绝而不猜测；弹窗预览 diff，按 1 / Enter 允许）。小型精确替换仍可用 `edit` / `multiedit`。
3. `bash go test ./internal/foo/...` 跑测试（弹窗审批）
4. 失败时自己分析输出再改一轮

复杂编辑的 `apply_patch` 输入使用一个完整补丁信封；每个 Update hunk 应保留足够的未改动行作为定位上下文：

```text
*** Begin Patch
*** Update File: internal/foo/bar.go
@@ func ProcessBatch() {
 func ProcessBatch() {
-    return oldResult
+    return newResult
 }
*** End Patch
```

`apply_patch` 只接受唯一的逐行精确上下文匹配。审批界面展示的就是实际执行计划；若审批期间目标发生外部变化，执行会拒绝而不会按过期 diff 写入。出现 `multiple matches`、`did not match` 或 `changed on disk since preview` 时，先用 `read` 重读更窄范围，并在 hunk 中增加上下文；不要改用 shell 做猜测性写入。

### 7.2 plan 模式探索仓库

```
ub --mode plan
> 这个项目的 agent loop 是怎么处理多 turn tool use 的？画一个流程图（不要改代码）
```

agent 只能用 read / ls / glob / grep / ask 和 plan 工具，不能改文件、不能跑命令、不能启动 sub-agent 或写 memory。研究完想动手时按 `Shift+Tab` 切到 `work`,或让 agent 通过 `exit_plan_mode` 请求批准退出。

### 7.3 Headless / CI 跑批

```sh
ub run -p "把 docs/*.md 里所有过期的 API 引用更新成新的命名" --mode work
```

- 没有交互；任何需要审批的工具调用会失败并打印到 stderr。CI 场景建议提前在 `<workspace>/.ub/permissions.yaml` 加好 `permissions.allow` 规则，或者配置 `approval_agent` + `--mode auto`。
- 退出码：0 成功；非 0 表示 agent 遇到错误或被拦截。

### 7.4 Resume 上次会话

```sh
ub --resume                  # 弹列表选
ub --resume=abc123           # 直接切到 session abc123
```

Resume 时会恢复 session 上次使用的 provider/model。mode 不随 session 恢复，使用当前 CLI/config 默认或本次启动传入的 `--mode`。旧版本 session 没有 provider 元数据时，ub 会按模型配置和远端模型列表尽力推断。

### 7.5 Rewind 到某条消息之前

```sh
/rewind
/rewind 4
```

`/rewind` 会列出当前 session 中历史 user message。选中某条后，ub 会删除该 turn 及之后的 rollout events，重建 TUI 显示和下一次请求上下文，并把选中的原始消息放回输入框，方便改写后重发。

ub 会在每个 user turn 开始前记录文件 checkpoint，并在 `write` / `edit` / `multiedit` / `apply_patch` 或可安全识别的 `bash` 删除（字面路径的 `rm` / `git rm`）真正执行前备份目标文件旧状态。`apply_patch` 会用已验证的补丁解析结果追踪新增、更新、删除和重命名涉及的路径。若当前 workspace 与目标 checkpoint 不一致，TUI 会先让你选择：只回退对话并保留当前 workspace 文件，或同时把 checkpoint 中可恢复的文件回到目标消息之前的状态。变量、通配符、命令内 `cd` 等无法可靠解析的 shell 删除不会进入文件历史；缺少可靠 checkpoint 的文件会保持不变并显示在提示里。

### 7.6 调试某次会话

```sh
ub rollout show abc123 | less          # 彩色 pretty-print
ub rollout show abc123 --json | jq .    # 机器可读
ub rollout show abc123 --turns 3..5     # 只看第 3-5 轮
ub rollout show abc123 --limit 100      # 最多输出 100 个事件
```

Pretty 输出会展开 `assistant_message` 中的结构化 content block：模型发起工具调用时会显示 `tool_use` 的工具名、调用 id 和 input JSON；工具执行结果继续显示为独立的 `tool_result` 事件。事件类型：`user_message` / `assistant_message` / `tool_result` / `summary` / `usage` / `activity` / `error`。

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
- `hover(file, line, col)`：查看当前位置说明
- `completion(file, line, col, max?)`：获取补全候选
- `document_symbols(file)`：列出文档符号树
- `rename(file, line, col, new_name)`：返回 rename 建议编辑，不直接写盘
- `code_action(file, line, col, end_line?, end_col?)`：列出 code action，不执行 action

ub 在 write / edit / multiedit / apply_patch 工具执行后会主动给仍存在的最终文件发 `didChange`，保证 diagnostics 反映最新内容。`rename` 和 `code_action` 只提供建议，真正修改仍由 agent 通过 apply_patch / edit / multiedit 走 diff 与权限流程。

## 10. Plan-then-execute

ub 提供 plan artifact 和 session todo 两层工作流:

- `plan_write` / `plan_update` / `plan_update_step` 管理可 review、可恢复的持久计划 artifact
- `todo_write` / `todo_update` 管理当前 session 的短生命周期执行清单,用于 TUI 中实时展示正在做什么

1. **进 plan 模式**(`Shift+Tab` 循环模式,`--mode plan` 启动,或 work 模式中由模型调用 `enter_plan_mode` 请求用户批准),让 agent 把思路 `plan_write` 成一个 markdown:

   ```
   plan_write(
     title="Migrate auth middleware",
     steps=["read existing middleware","grep call sites","write new middleware","update tests","run go test"],
     notes="see issue #128 for compliance requirements"
   )
   ```

   `plan_write` 只在 plan 模式暴露和执行;work/auto 模式不会让模型创建新 plan。plan 模式会给模型注入规划约束,并只暴露 read / ls / glob / grep / ask / plan_write / plan_update / exit_plan_mode。工具会在 `$XDG_STATE_HOME/ub/plans/<project-key>/<时间戳>-<slug>.md` 写一个文件,并把 `plan_id` 和绝对 `path` 返回。TUI 完成行会直接显示 `Wrote plan <plan-id>` / `Updated plan <plan-id>`,也可以用 `/plans` 列出当前 workspace 的所有 plan artifact。

   plan 准备好后,agent 应调用 `exit_plan_mode(plan_id, summary?)` 请求批准退出;批准后恢复进入 plan 前的本进程 mode,拒绝则留在 plan 模式并继续用 `plan_update` 修订。`exit_plan_mode` 缺少 `plan_id` 时不会弹批准框,会返回 tool error 让模型先创建或更新 plan artifact。

2. **修订已有 plan**:如果你在 plan 模式纠正了模型的计划,agent 应使用 `plan_update` 原地更新同一个 `plan_id`,而不是再次调用 `plan_write` 创建新 plan:

   ```
   plan_update(
     plan_id="20260527T..",
     steps=["re-read middleware boundary","patch only auth middleware","run focused auth tests"],
     reason="user narrowed scope"
   )
   ```

   也可以在 TUI 中直接 review/edit plan artifact:

   ```
   /plans
   /plan-edit 20260527T..
   ```

   `/plans` 会打开可筛选的 plan picker,回车后用 `$VISUAL` / `$EDITOR` 打开所选 markdown;`/plan-edit <plan-id>` 和 `/plans <plan-id>` 则直接打开 `$XDG_STATE_HOME/ub/plans/<project-key>/<plan-id>.md`,编辑器退出后回到 TUI。

3. **切到 work 模式**继续会话。agent 用 `read` 看 plan,按顺序执行;每完成一步调 `plan_update_step`:

   ```
   plan_update_step(plan_id="20260527T..", step_index=2, status="done", note="found 12 call sites")
   ```

   状态机:`in_progress`(`[>]`)/ `done`(`[x]`)/ `skipped`(`[~]`)/ `failed`(`[!]`)/ `pending` 回退(`[ ]`)。当所有步骤都进入终态(`done` / `skipped` / `failed`)时,文件顶部的 `Status:` 自动变 `complete`,并在 `## Log` 末尾追加一条带时间戳的记录。

4. **普通多步骤任务用 todo**:不需要持久 plan 的任务可以直接在 work/auto 模式创建执行清单:

   ```
   todo_write(items=[
     {"id":"inspect","content":"read existing middleware","status":"in_progress"},
     {"id":"patch","content":"patch auth middleware"},
     {"id":"test","content":"run focused tests"}
   ])
   ```

   每完成一步更新 todo:

   ```
   todo_update(id="inspect", status="completed", note="found auth boundary")
   todo_update(id="patch", status="in_progress")
   ```

   todo 状态为 `pending` / `in_progress` / `completed` / `skipped` / `failed`,同一清单最多一个 `in_progress`。TUI 会把 `todo_*` 的 tool result 抽取成独立 Todo checklist,工具 block 只保留审计行;`todo_update` 原地刷新当前 checklist,新的 `todo_write` 会把 checklist 移到最新工具事件附近。rollout 也会记录这些 tool result,所以 resume / `ub rollout show` 能看到历史执行清单。todo state 存在 `$XDG_STATE_HOME/ub/todos/<session-id>.json`,不写入工作区、不复用 plan markdown 的 checkbox。

5. **中断恢复**:下次会话从上次 tool result / rollout 中取回 `path` 后可直接 `read` 这个 state-root 下的 plan,或用 `plan_update_step(plan_id="...")` 继续标记进度。plan artifact 是用户 state 数据,不会写入工作区或参与 git。session todo 会随 session id 保存在 state-root 下,可继续用 `todo_update` 更新。

## 11. Goal mode

Goal mode 面向"请你围绕这个目标连续推进"的长任务。它不替代 plan/todo:

- **goal**：跨 turn 的 objective、预算、状态和停止条件。
- **plan**：可 review/edit 的持久方案 artifact。
- **todo**：当前 session 的实时执行清单。

TUI 内可以用 slash command 创建或查看 goal:

```text
/goal 修复当前项目里和上下文压缩相关的两个剩余 TODO,并更新文档
/goal
/goal clear
```

创建后,TUI 状态栏会显示 goal 状态。agent 应通过 `get_goal()` 查看目标和预算,工作完成且验证通过后调用 `update_goal(status="complete")`;如果连续遇到同一个阻塞条件,才用 `update_goal(status="blocked", block_reason="...")`。

无头模式可以直接跑:

```bash
ub goal -p "修复 flaky tests 并跑完整验证" --turn-budget 12 --token-budget 200000
```

`ub goal` 会预创建 session-scoped goal,然后自动续跑 agent turn,直到 goal 进入 complete / blocked / paused,或达到 turn/token 预算。goal state 保存在 `$XDG_STATE_HOME/ub/goals/<session-id>.json`;删除 session 时会随 session artifacts 一起清理。

## 12. Agent loop 上限

主 agent 默认不按固定 tool-use 轮数截断；它会持续到模型停止调用工具、用户中断、provider 出错、上下文触发压缩/失败，或命中重复工具循环保护。这样长任务不会因为一个小的默认 cap 在中途停住。

如果希望给 CI、批处理或自动化任务加硬保护，可以在配置里设置正整数 `max_turns`：

```yaml
max_turns: 80
```

触顶后，TUI 会询问是否继续给一段额外预算；无头模式会发起一次禁用工具的收尾请求，让模型用已获得的信息回答。重复调用相同工具并拿到相同结果时，agent 会提前触发同样的禁用工具收尾路径。

## 13. Subagents(派发子任务)

`task(prompt, max_turns?)` 工具让主 agent 派发一个**子 agent**跑一个 self-contained 子任务,把子任务的最终回答作为 tool result 拿回来。典型用法:

- "去 grep 一下 internal/lsp/ 里所有用到 deprecated API 的地方,列出 file:line 给我"
- "调研一下 docker-compose 里这两个服务的依赖关系,写一段 200 字总结"

子 agent 与主 agent **共享 provider 与工具集**,但**独立 conversation context**(因此可以隔离掉调研期间的中间状态,避免污染主 prompt)。agent 执行器本身保持轻量无状态:provider/client 通过 CLI runtime cache 复用,主/子 Agent 通过 `agent.Factory` 从共享构造模板新建,状态通过父 session / rollout / tool result 外置保存。本版本有以下取舍:

- **深度上限 = 1**:子 agent 里再调 `task` 会被工具直接拒绝(避免递归 token 爆炸)
- **`max_turns` 只限制子 agent**:省略时继承当前 agent 默认,即默认不按步数截断;传入正整数时只对该子任务生效
- **不创建独立 session**:子 agent 不刷 session 列表;最终回答仍作为父 `task` tool result 返回
- **父 turn 可观测**:子 agent 的 start/done、tool lifecycle、permission 等 display-only activity 会镜像进父 rollout/TUI,带 `subagent:` 前缀并用 `subagent:<parent-task>:<child-tool>` 命名空间隔离;恢复 session 时这些 activity 仍可见
- **reasoning 只 live 展示**:子 agent reasoning delta 只走 TUI live activity,不逐片持久化进 rollout,避免长推理把父 session 数据库刷大

后续 §4-01 agent loop 解耦完成后,会扩成"子 agent 独立模型 / 工具集 / TUI 多 pane"等完整能力。

## 14. Workspace 持久记忆

ub 维护两类 durable memory,并另外加载工作区指令。每次 agent 开跑时都会注入到 system prompt:

1. **全局指令** `~/.config/ub/instructions.md` —— 人工编写的跨项目偏好(跟着用户走)
2. **自动记忆** `$XDG_STATE_HOME/ub/memory/<project-key>/memory.md` —— 机器自动追加的项目事实(不在 git 中,按项目隔离)
3. **工作区指令** `<workspace>/AGENTS.md` —— 人工编写的团队事实(跟着项目走,可 git commit),由 `prompt.workspace_instructions` 单独控制

### 写入方式

**`remember` 工具** —— `remember(text="...", category="...", scope="auto" 或 "global")`

- `scope="auto"`(默认):写入项目自动记忆,按 project-key 隔离
- `scope="global"`:追加到全局指令文件,不会重写已有手写内容
- `category`:条目分类,影响注入优先级(见下表)

| Category | 含义 | 注入优先级 |
|----------|------|-----------|
| `preference` | 用户偏好 | 高(始终注入) |
| `project` | 项目事实 | 高 |
| `pattern` | 代码风格/模式 | 中 |
| `decision` | 架构决策 | 中 |
| `general` | 杂项(默认) | 低 |
| `debug` | 调试笔记 | 低(最先被截断) |

**`recall` 工具** —— `recall(query="...", category="...")`

按关键词和分类检索自动记忆,不需要全量读取。

### 去重

自动记忆同一 category 下相似条目或同主题冲突条目会自动合并(更新时间戳),不会重复写入。`debug` 和 `general` 等低优先级条目会随时间衰减;条目过多时按 category 优先级和新旧裁剪。写入前会拒绝明显 credential/token/private-key 以及临时 debug/stack trace 内容。全局指令是 append-only,用于保护手写内容。

每次成功写入都会记录一个 `memory_write` rollout 事件,便于 `ub rollout show` 和 session 搜索审计来源、scope、category、action 与写入路径。

### 注入预算

`config.yaml` 的 `memory.max_chars`(默认 4000)控制全局指令和自动记忆。超过预算时按 category 优先级截断自动记忆:

1. 全局指令**始终保留**
2. 自动记忆按 preference → project → pattern → decision → general → debug 排序
3. 同 category 内最新条目优先

`AGENTS.md` 不受 `memory.max_chars` 控制,而是使用 `prompt.workspace_instructions.max_chars`。

```yaml
memory:
  max_chars: 8000
  auto:
    enabled: true
    trigger: background
    max_candidates: 3
    max_prompt_chars: 12000
    min_turns_since_extraction: 3
    min_new_messages: 6
    min_interval: 10m
    drain_timeout: 3s
    disable_on_external_context: true
```

### 自动归纳

默认启用 `memory.auto.enabled`。成功完成的 work / auto / full-access turn 结束后,ub 只在主流程里做低成本观察:plan 模式、空 workspace、显式 `remember` 已处理的 turn、以及默认配置下使用外部上下文(MCP / web / tool_search 类工具)的 turn 都不会触发自动写入。其余 turn 会进入后台调度器;调度器按显式记忆信号、`memory.auto.min_turns_since_extraction`、`memory.auto.min_new_messages` 和 `memory.auto.min_interval` 批量决定何时调用当前 provider 可用的 `small_model`(未配置或该 provider 明确不可用时复用当前模型)抽取长期事实。

`memory.auto.trigger=background` 是默认模式,会把抽取放到后台,避免每个 turn 结束都同步等待 small model。`trigger=immediate` 可用于调试或希望每个 eligible turn 都尽快后台抽取的场景。headless `ub run` 在主答案输出后最多等待 `memory.auto.drain_timeout`,TUI 则先展示完成状态,后台抽取结果随后通过 `memory_write` 事件审计。

自动归纳只接受受控 JSON 候选,随后仍会经过 category 校验、隐私过滤、冲突合并和衰减策略;被拒绝的候选不会写入 memory。

## 15. 长输出落盘(spillover)

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

`bash` 的 result content 顶部有一个稳定的 `<shell_metadata>` 块,字段:

```
<shell_metadata>
exit_code=0
duration_ms=42
timeout=true        # 仅在超时 kill 时出现
aborted=true        # 仅在 ctx 取消 kill 时出现
error=<msg>         # 启动失败或其他错误
</shell_metadata>
```

`job_output(job_id, tail?)` 返回当前 job 快照:

```
job_id=<id>
state=running|exited
exit_code=<code>
stdout_total=<bytes>
stderr_total=<bytes>
--- stdout ---
...
--- stderr ---
...
```

`job_output(job_id, follow=true, timeout_ms?)` 会通过 streaming partial output 先推当前 ring buffer 快照,再推新增 stdout/stderr,直到 job 退出、`timeout_ms` 到期或请求取消。最终 result 仍是上面的快照格式;如果 follow 自身到期,会额外包含 `follow_timeout=true`。

## 16. Hooks(生命周期 shell 钩子)

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
      tools: ["apply_patch", "edit", "write", "multiedit"]
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

## 17. 故障排查

| 症状 | 原因 / 解决 |
|---|---|
| `provider "xxx" not configured` | `ub config show` 看合并后的 providers 是否包含 xxx；`default_provider` 必须与某个 provider key 匹配 |
| 401 Unauthorized | 环境变量未注入；`echo $OPENAI_API_KEY` 看是否为空；YAML 里写 `${OPENAI_API_KEY}` 而不是硬编码 key 字面值 |
| 本地 vLLM / Ollama 连不上 | `ub doctor --plain` 看探测结果；`curl <base_url>/models`（OpenAI 兼容；Ollama 用 `/v1/models`） |
| 模型不支持 tool calling | `ub doctor` 会标注；本地小模型常见，换支持 tool calling 的模型，或改用 `ub chat` 单轮模式 |
| TUI 内 Enter 没反应 | 检查是不是停在中文输入法候选状态；权限弹窗里需要先方向键选 / 直接按数字键 |
| 鼠标点击不响应 / 块没法展开 | 确认没有 modal / 选择器开着（按 `Esc` 关掉）；如终端不支持鼠标，用 `Ctrl+O` / `Ctrl+N` / `Ctrl+P` + `Enter` 替代 |
| 切到 plan 模式后 write/bash/task/memory 工具报错 | 这是预期：plan 模式只允许 read / ls / glob / grep / ask / plan_write / plan_update / exit_plan_mode；切回 `work` 或批准 `exit_plan_mode` |
| 长会话越来越慢 | 主动 `/compact`；调小 `context.tool_results.max_chars`；切到更长 context 的模型 |
| context 百分比与后端实际窗口不符 | 优先在模型配置中设置 `max_context_tokens`；也可删除 `$XDG_STATE_HOME/ub/context-windows/` 下的派生缓存，让 ub 从静态能力重新学习 |
| 在 `/proj/sub` 看不到 `/proj` 的 session | sessions 按 cwd 字符串隔离；`cd` 到原 cwd 再 `ub --resume` |
| 想检查 prompt 组成 | `ub prompt inspect` 查看 section manifest；确需正文时使用 `ub prompt inspect --show-content`（可能包含项目指令或 memory） |
| Agent 卡在某轮不出来 | `Esc` 中断当前轮；如果是 plan 模式下试图写文件，先看 tool error 信息再 retry |
| rollout 数据库被锁 | SQLite WAL 模式下少见，多半是另一个 ub 进程没退干净；`ps -ef | grep ub` 检查 |
| 命令未被审批就直接被拒 | 黑名单命中（`rm -rf /` 类）；这是有意拦截，参数稍微改一下（如显式列举目录）就能正常进入审批 |

更多上下文（架构 / 设计决策）参见 [`design.md`](design.md)；产品边界参见 [`requirements.md`](requirements.md)。
