## Why

`roadmap-v2.md` §3-02 把 workspace 持久记忆列为 L 工程量条目。本 change 交付
**最小可用版本**:显式写入(`remember` 工具)+ 每轮注入到 system prompt。
LLM-driven auto-write 留给后续 change(成本/收益尚需博客评估)。

最小版能立刻解决一个痛点:agent 在多轮会话之间一遍又一遍重新发现"build 命令是 X、测试在哪、issue #42 的修复在 auth.go:120"等事实。把这些工件落到 `.ub/memory.md` 与 `~/.config/ub/memory.md`,model 每次起步都能拿到。

## What Changes

- 新建 `internal/memory/` 包:
  - 两个 scope: `workspace`(`<workspace>/.ub/memory.md`)与 `global`(`~/.config/ub/memory.md`)
  - `Append(workspaceRoot, scope, text) error`:在对应文件末尾追加一个带 `## YYYY-MM-DD HH:MM:SS Z` 标题的条目;文件不存在时创建;父目录缺失时 mkdir
  - `Read(workspaceRoot, maxChars int) string`:读 global 在前、workspace 在后(workspace 优先意味着覆盖性更高,但本版只是拼接),全文超过 `maxChars` 时按"尾部优先(最新条目)"截断,头部加 `... [memory truncated]\n` 一行
- 新建 `internal/tool/memory/`:
  - `remember(text, scope?)`:scope ∈ {workspace, global},默认 workspace;Risk = safe(目标是 ub 元数据 .ub/ 或 user config,不是用户代码)
  - 调用 `memory.Append`,result 含写入文件路径与新条目时间戳
- `internal/agent`:
  - `Options.WorkspaceRoot string` 字段(若空则不注入 memory)
  - 新增 `withMemoryContext`,在 `withRuntimeContext` 之后(或之前)插入一条 `RoleSystem` 消息,内容为 `<workspace_memory>\n<内容>\n</workspace_memory>`;空时不插入
  - 注入预算从 `cfg.Memory.MaxChars`(默认 4000)读取
- `internal/config/types.go` 新增 `MemoryConfig`:
  ```yaml
  memory:
    max_chars: 4000
  ```
- `internal/cli/root.go` / `tui.go`:把 cwd 透过 `WorkspaceRoot` 传进 Agent.Options,把 `remember` 工具加进注册链
- `docs/usage.md` 加 memory 章节

## Capabilities

### New Capabilities

- `workspace-memory`:memory 文件路径、Append/Read 语义、`remember` 工具规格、agent 注入语义

## Impact

- 新增 `internal/memory/` + 测试
- 新增 `internal/tool/memory/` + 测试  
- 修改 `internal/agent/agent.go`(Options 字段、新增 memory 注入)与 `runtime_context.go`
- 修改 `internal/config/types.go` + `merge.go`
- 修改 `internal/cli/root.go` 与 `tui.go`
- 不引入新依赖
- 不改 rollout schema(memory 写入暂不发自定义事件;改了哪个文件 tool 自身的 Result.Files 已经够 TUI 看到)
- breaking change:无;memory 默认关闭(WorkspaceRoot 为空就不注入)
