## Context

`add-tool-registry` 已经把 `Tool`、`Registry`、`PreviewableTool`、`Preview`、`FileDiff`、`Result` 定义好。本 change 给出第一批具体工具实现，把抽象接口跑通；同时把 fs 沙箱规则定下来，避免后续 grep/bash 重复造轮子。`docs/design.md §4` 已经描述了五个工具的语义，本 change 把它们落到代码 + 测试 + spec。

## Goals / Non-Goals

**Goals:**

- 五个工具实现完整：read / ls / glob / write / edit。
- `write` 和 `edit` 实现 `PreviewableTool`，Preview 必须读现盘 + 在内存里模拟改动 + 用 unified diff 输出，**不写盘**。
- 统一的路径校验函数：所有工具入口 MUST 经过同一函数 normalize + 沙箱判定。
- `fs.Register(reg, root)` 是唯一对外注册入口，避免上层逐个挂工具时漏掉。
- 全部用 `t.TempDir()` 跑单测，无外部依赖。

**Non-Goals:**

- 不实现 `multiedit`（V1 暂缓，留到 Sprint 2 后续 iteration）。
- 不实现权限审批 / dispatcher / mode gate（`plan` 模式拒 write 由 dispatcher 在 I-20/I-21 处理）。
- 不做文件锁；并发写盘问题在 I-21 的 agent loop 串行化 tool call 后自然消失。
- 不处理符号链接的“跨 workspace 跳出”问题（V1 直接 `filepath.Clean` 判定 prefix；后续若发现实际项目使用 symlink 再补）。
- 不实现内容编码探测；read 默认按 UTF-8 处理，二进制文件返回错误（按 line 切分失败时报错）。

## Decisions

- **依赖选型：**
  - `glob` 用 `github.com/bmatcuk/doublestar/v4`：标准 `path/filepath.Glob` 不支持 `**`；doublestar 是 Crush / opencode 等参考项目的共同选择。
  - `edit` 的 unified diff 用 `github.com/aymanbagabas/go-udiff`：`docs/design.md §4` 已经点名；纯 Go 实现、无 CGO。
  - 不引入 `go-difflib` 等额外 diff 库；`write` 也用同一份 `go-udiff`，保持输出格式一致。
- **`fs.Register(reg *tool.Registry, root string) error`：** 一次注册五个工具；root 在注册期固定为 `filepath.Clean(root)`，所有工具实例共享。这样后续配置变化（如果 V1 后期把 workspace 改成可配置）只需要在注册前算好 root，工具内部不需要重新读 config。
- **路径校验 `resolve(root, path) (string, error)`：**
  1. `filepath.Clean(path)`；
  2. 如果是相对路径，相对 root 解析为绝对；如果是绝对路径，原样使用；
  3. `filepath.Rel(root, abs)` 得到相对部分，若以 `..` 开头 → 拒绝；
  4. 返回 clean 后的绝对路径。
  这样能同时拒绝 `../etc/passwd`、`/etc/passwd` 和 symlink 之外的逃逸尝试。
- **`read` 行号格式：** 类似 `cat -n`，行号宽度按文件总行数对齐。`offset`（从 1 开始）和 `limit` 都可选；不传则返回整文件，但超过 2000 行强制截断并在尾行加 `... (truncated, use offset/limit)` 标记，避免单次 tool result 撑爆 token。
- **`ls` 输出：** 一行一个条目，格式 `kind\tname`，`kind` 取 `dir` / `file` / `symlink` / `other`。简单文本以便 LLM 解析，又避免被它误解析成 JSON。
- **`glob` 输入：** `pattern` 为相对 root 的 doublestar 表达式；返回的路径也是相对 root，按字符串字典序排序，保证 deterministic。
- **`write` Preview 与 Execute：**
  - Preview：读现盘 → 若不存在 → `Kind=create`、unified diff 为“空文件 → 新内容”；若存在 → `Kind=modify`、unified diff 为“现盘 → 新内容”。`Summary` 形如 `Write foo.go: create` 或 `Write foo.go: modify (+12/-3)`。
  - Execute：`os.MkdirAll` 父目录后 `os.WriteFile` 覆盖写；Result 的 `Files` 中给一条与 Preview 一致的 `FileChange{Path, Kind}`。
- **`edit` Preview 与 Execute：**
  - 必填：`path`、`old`、`new`；可选 `replace_all bool`。
  - 读现盘 → `strings.Count(content, old)`：
    - 等于 0 → 错误 `edit: old string not found`。
    - 大于 1 且 `replace_all=false` → 错误 `edit: 3 matches, set replace_all=true to replace all`。
    - 否则按 `strings.Replace(content, old, new, -1 or 1)` 应用。
  - Preview 使用 `go-udiff.Unified(name, name, before, after)`，`Kind=modify`。
  - Execute：先校验现盘内容仍与 Preview 时一致（避免并发改动）→ 不一致返回错误；一致则 `os.WriteFile` 覆盖。
- **`Result.Content` 设计：** read 直接返回带行号文本；ls/glob 返回多行文本；write/edit 返回简短确认（`wrote foo.go (N bytes)` / `edited foo.go (1 replacement)`），让 LLM 能感知执行结果但不用解析 JSON。

## Risks / Trade-offs

- **`go-udiff` API 变动风险** → 锁版本在 `go.mod`；如果未来要换实现，diff 字符串接口只是 string，调用方不感知。
- **`edit` 的“先读再写”有 TOCTOU 风险** → Execute 阶段再读一次现盘对比，避免覆盖并发改动；不引入文件锁。
- **大文件 read 截断阈值（2000 行）是硬编码** → 后续可以根据上下文窗口大小动态调整，但 V1 先固定，配合 LLM 主动用 offset/limit。
- **路径校验拒绝 symlink 跨越是有缝的**（V1 不解析 symlink target）→ V1 把 workspace 当受信目录；后续可在 dispatcher 层补强。
- **doublestar 与 `**` 的递归遍历可能很慢** → V1 不做并行/超时控制；后续若实际项目卡，再加上下文 cancel。所有工具都接受 `ctx`，但 V1 实现暂不在循环里轮询 `ctx.Done`，留为 TODO 标记给 I-21。
