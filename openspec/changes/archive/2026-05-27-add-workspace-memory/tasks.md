## 1. memory 包

- [x] 1.1 `internal/memory/memory.go`:
  - `ScopeWorkspace` / `ScopeGlobal` 常量
  - `Path(workspaceRoot, scope) (string, error)`:返回两个 scope 对应的绝对路径(workspace = `<root>/.ub/memory.md`,global = `~/.config/ub/memory.md`)
  - `Append(workspaceRoot, scope, text string) (path string, entryHeader string, err error)`
  - `Read(workspaceRoot string, maxChars int) string`:拼接两 scope 文件,头部加 `<!-- global memory --> ... <!-- workspace memory -->` 注释帮助 model 区分;超 maxChars 时从开头丢弃直到 ≤ maxChars,丢弃部分位置加 `... [memory truncated]\n`
- [x] 1.2 `internal/memory/memory_test.go`:Append 多次后顺序保持;Read 在缺文件时返回空串而非错误;Read 超 cap 截断;Path 沙箱(workspaceRoot 为空时返回错误)

## 2. remember 工具

- [x] 2.1 `internal/tool/memory/remember.go`:
  - args:`{text string, scope string?}`(默认 workspace;`global` 是另一选项)
  - 校验非空 text;scope 合法值
  - 调用 `memory.Append`,返回 `Result.Content = "remembered: <path>\n## <header>"`、`Result.Files` 含 KindModify(或 KindCreate)
- [x] 2.2 `internal/tool/memory/register.go`:`Register(reg, workspaceRoot)`
- [x] 2.3 `internal/tool/memory/memory_test.go`:happy path workspace、global、空 text 拒绝、非法 scope 拒绝

## 3. agent 注入

- [x] 3.1 `agent.Options.WorkspaceRoot string` 字段
- [x] 3.2 `agent.Options.Memory MemoryConfig` 或直接 `MemoryMaxChars int`(简化)
- [x] 3.3 `runtime_context.go` 或新文件:`withMemoryContext(messages, workspaceRoot, maxChars)` 在 messages 头部插入 system message `<workspace_memory>\n<read 结果>\n</workspace_memory>`;空时不插入
- [x] 3.4 在 `withRuntimeContext` 之后调用,或合并到 `prepareMessages`
- [x] 3.5 agent_test:断言注入存在;断言 WorkspaceRoot 为空时不注入

## 4. config

- [x] 4.1 `internal/config/types.go`:`MemoryConfig{ MaxChars int }`,挂到 `Config.Memory`
- [x] 4.2 `internal/config/merge.go`:加 mergeMemory
- [x] 4.3 `internal/config/types.go`:`Defaults()` 不预填(0 → agent 用 4000)

## 5. cli 接线

- [x] 5.1 `cli/root.go` `newToolRuntime` 把 memory.Register 加入注册链
- [x] 5.2 `cli/root.go` 与 `cli/tui.go` 的 agent.New 调用补 `WorkspaceRoot` 与 `MemoryMaxChars`

## 6. 文档

- [x] 6.1 `docs/usage.md` 新增 Memory 小节
- [x] 6.2 `docs/design.md` 工具列表补 `remember`
- [x] 6.3 `openspec/changes/add-workspace-memory/specs/workspace-memory/spec.md`

## 7. 验证

- [x] 7.1 `go test ./...`
- [x] 7.2 `make lint`
- [x] 7.3 `make build`
- [x] 7.4 `make schema`
