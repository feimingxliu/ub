# streaming-tools Specification

## Purpose
TBD - created by archiving change add-streaming-tools. Update Purpose after archive.
## Requirements
### Requirement: StreamingTool 接口

`internal/tool` 包 SHALL 暴露一个新接口 `StreamingTool`,继承 `Tool`(必须先实现现有的 `Execute` 方法作为 fallback),多一个方法:

```go
ExecuteStream(ctx context.Context, args json.RawMessage, events chan<- StreamEvent) (Result, error)
```

实现该接口的工具 MUST 在执行期间通过 `events` 推送 zero 或多条 `StreamEvent`,然后在执行完成时 close events 之前返回(返回前 close events 由 caller 负责;实现 MUST NOT close events 本身)。`StreamEvent` MUST 含两个字段:

- `Kind StreamEventKind`:`stdout` / `stderr` / `info`
- `Data string`:UTF-8 文本片段;长度由工具决定

未实现 StreamingTool 的工具 MUST 保持现有 `Execute` 协议不变;agent runtime 不应假设 streaming。

#### Scenario: events chan 由调用方关闭

- **GIVEN** 一个 StreamingTool 实现
- **WHEN** ExecuteStream 推送完所有 StreamEvent 并返回 Result
- **THEN** 实现 MUST NOT 自己 close(events);agent runtime 负责在 goroutine 退出后 close

#### Scenario: 仍需实现 Execute

- **GIVEN** 一个声称是 StreamingTool 的工具
- **WHEN** 编译期接口断言运行
- **THEN** 它 MUST 同时实现 `Execute`(StreamingTool 嵌入了 Tool 接口)

### Requirement: agent runtime 转发 partial output

agent runtime MUST 在 `runTool` 中先尝试 `t.(StreamingTool)`;断言成功时:

1. 启动一个带 buffer(>= 64)的 events chan
2. 在 goroutine 中执行 `ExecuteStream`;每收到一条 `StreamEvent`,转换为 `Event{Type: EventToolPartialOutput, ToolUseID, ToolName, Content}` 并经由 `a.emit` 发送给当前 EventSink
3. `Content` 字段在 emit 前 MUST 被截到 `streamPartialMaxBytes`(4KB);截断时 MUST 在尾部追加 ` ... [chunk truncated]`
4. 等待 ExecuteStream 返回得到 `Result`;后续的 permission / spillover / activity 处理与非 streaming tool 完全一致

#### Scenario: streaming tool emit partial events

- **GIVEN** 一个 StreamingTool 在 ExecuteStream 中推送 3 条 stdout StreamEvent 之后返回 success
- **WHEN** agent.runTool 调度该工具
- **THEN** agent 的 EventSink MUST 至少收到 3 条 `Event{Type: EventToolPartialOutput}`,且每条 Event.ToolUseID 等于本次 tool call 的 ID

#### Scenario: 非 streaming tool 不发 partial 事件

- **GIVEN** 一个只实现 `Execute`(未实现 ExecuteStream)的工具
- **WHEN** agent.runTool 调度它
- **THEN** EventSink MUST NOT 收到任何 `EventToolPartialOutput` 事件

### Requirement: bash 工具实现 StreamingTool

`bash` 工具 SHALL 实现 `StreamingTool`。`ExecuteStream` MUST 在收到 stdout 或 stderr 字节时,在写入 capWriter 的同一时刻向 events chan 推送一条 `StreamEvent`(`Kind` = `stdout` 或 `stderr`,`Data` 是该写入批次的 UTF-8 安全文本)。最终 `Result` 与 `Execute` 方法的格式完全一致(包含 `<shell_metadata>` 块、`--- stdout ---` 与 `--- stderr ---` 段)。

#### Scenario: bash partial stdout

- **GIVEN** 命令 `/bin/sh -c 'printf a; sleep 0.05; printf b; sleep 0.05; printf c'`
- **WHEN** 通过 `ExecuteStream` 跑
- **THEN** events chan MUST 至少推送 2 条 `Kind=stdout` 的 `StreamEvent`(分批),且 `Result.Content` 中的 `--- stdout ---` 段 MUST 是 `abc`

