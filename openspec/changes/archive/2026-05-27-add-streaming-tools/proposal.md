## Why

`roadmap-v2.md` §3-06 把 tool I/O streaming 列为 V2 主体 L 项之一。当前
`tool.Tool.Execute` 同步阻塞,长 tool(`go test ./...`、`docker build`)期间
TUI 死等;模型也只能在命令完整跑完后才看到输出。

本 change 用 **opt-in 的 `StreamingTool` 接口**(非 breaking 扩展)解决这个
问题,首批接入 `bash` 工具,其它工具保持现状 —— 这样既能让最痛的 bash 路径
立刻获益,也保留后续给 `job_output --follow`、`lsp.diagnostics --watch` 接入
流式的空间。

## What Changes

- `internal/tool/tool.go` 新增:
  - `StreamEvent` 类型(`Kind` ∈ `stdout` / `stderr` / `info`、`Data string`)
  - `StreamingTool` 接口,继承 `Tool` 并多一个方法
    `ExecuteStream(ctx, args, events chan<- StreamEvent) (Result, error)`
- `internal/agent/event.go`:新增 `EventToolPartialOutput EventType`,Event
  字段复用 `ToolUseID` / `ToolName` / `Content`(stdout/stderr chunk)
- `internal/agent/agent.go` `runTool`:
  - 工具如果实现 `tool.StreamingTool`,起一个 1024 缓冲的 chan,
    在新 goroutine 里 `ExecuteStream`;每读到一个 StreamEvent,
    通过 `a.emit(Event{Type: EventToolPartialOutput, ...})` 转发
  - chunk 写入时如发现内容 > `streamPartialMaxBytes`(4KB),截到 4KB
  - 非 streaming tool 保持原 `Execute` 路径不变
- `internal/tool/shell/bash.go`:实现 `ExecuteStream`,复用现有
  `assembleContent` / `capWriter`,但 stdout / stderr 在被 `capWriter`
  抓取的同时也写到 events chan(用一个 fan-out writer)
- `internal/tui`:新增对 `EventToolPartialOutput` 的渲染 hook —— 本 change
  只在 agent 层 emit 事件,TUI 的滚动预览 UI 留到下一个 change(避免与
  diffview 现有渲染纠缠)
- `docs/usage.md` 加一段说明:streaming 工具现在仅有 bash;其余 tool 仍
  同步阻塞

## Capabilities

### Modified Capabilities

- `tool-protocol`:在 Tool 接口之外新增 StreamingTool 可选接口,定义
  StreamEvent 类型与 agent runtime 对它的处理流程

## Impact

- 修改 `internal/tool/tool.go`(新类型与接口)
- 修改 `internal/agent/agent.go`(runTool 分支)与 `event.go`(新事件类型)
- 修改 `internal/tool/shell/bash.go` 实现 ExecuteStream
- 新增 streaming 相关单测
- 不改 rollout schema(partial output 是临时事件,不写 rollout)
- 不引入新依赖
- breaking change:无;StreamingTool 是可选接口,旧工具仍按 Tool 工作
