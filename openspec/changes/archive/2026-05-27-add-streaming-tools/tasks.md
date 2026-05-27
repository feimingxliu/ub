## 1. tool 接口扩展

- [x] 1.1 `internal/tool/tool.go`:新增 `StreamEvent{Kind StreamEventKind, Data string}` 与 `StreamEventKind` 常量(`stdout` / `stderr` / `info`)
- [x] 1.2 `internal/tool/tool.go`:新增 `StreamingTool` 接口,签名 `ExecuteStream(ctx, args, events chan<- StreamEvent) (Result, error)`
- [x] 1.3 注释解释:实现了 StreamingTool 的工具仍要实现 `Execute`(用作 fallback);agent 内部优先走 ExecuteStream

## 2. agent 集成

- [x] 2.1 `event.go`:新增 `EventToolPartialOutput EventType = "tool_partial_output"`
- [x] 2.2 `agent.go` `runTool`:在 `t.Execute(ctx, args)` 之前 type-assert StreamingTool;若是,起 events chan + goroutine forward 到 `a.emit`(EventToolPartialOutput),阻塞等待 ExecuteStream 返回
- [x] 2.3 chunk 大小上限 4KB:emit 之前对 Data 字段截断;截断时附加 `... [chunk truncated]`
- [x] 2.4 agent_test:写一个 fake StreamingTool,断言 agent 接到的 events 含 stdout chunk

## 3. bash 实现 StreamingTool

- [x] 3.1 `bash.go`:`ExecuteStream` 复用 `Execute` 主体,但把 stdout / stderr 用 multi-writer 接到 capWriter + events chan(实时一行一推或缓冲一定字节再推)
- [x] 3.2 `bash_test.go`:跑一个 `printf "a\nb\nc\n"`,断言收到至少 3 个 StreamEvent(可以合并,但要至少看到第一条早于命令结束)
- [x] 3.3 timeout / aborted 路径也要正常发完整 metadata(StreamingTool 仍然返回完整 Result)

## 4. 验证 + 文档

- [x] 4.1 `docs/usage.md` / `docs/design.md`:streaming tool 协议说明
- [x] 4.2 `openspec/changes/add-streaming-tools/specs/streaming-tools/spec.md`
- [x] 4.3 `go test ./...`
- [x] 4.4 `make lint`
- [x] 4.5 `make build`
