## Why

`tooloutput.LimitResult` 已经把超长 tool 输出落到
`<state-root>/tool_outputs/<sessionID>/<toolUseID>.txt`,并把绝对路径写进
result content 的 footer。模型当下可以直接用 `read(path=...)` 再看,但有两个痛点:

1. footer 里的 path 信息会随着上下文压缩(`auto-summary`)消失;再过几轮后
   模型只剩下"很久之前那次 bash 输出"的印象,而不知道还能找回原始内容
2. 模型每次想看历史 tool 输出都得把绝对路径背下来再 read,不直观;
   `tool_use_id` 才是 rollout 中工具调用的稳定标识

S3-08 把"事件日志的反向消费"做成一个一等公民工具:把 `tool_use_id`
作为入参,工具自己定位 spillover 文件并以 `read` 一致的格式返回。

## What Changes

- 新增 `tool_result` 工具,`Risk=safe`,只读 spillover 文件
- 入参:`{tool_use_id: string, offset?: int, limit?: int}`,语义与 `read` 一致
- 内部:从 `context.Context` 取出当前 session 的 ID(由 agent 调用前注入),
  计算 `<output-root>/<safe-sessionID>/<safe-toolUseID>.txt`,以 `read` 工具
  的输出格式(带行号、offset/limit、默认上限同 `read.ReadMaxLines`)返回内容
- session id 注入机制:
  - `internal/tool` 新增 `WithSessionID(ctx, sid)` / `SessionIDFromContext(ctx)`
  - `agent.runTool` 在调用 `Execute` 前 `ctx = tool.WithSessionID(ctx, sessionID)`
- spillover 路径推导:`internal/tooloutput` 把内部 `safePathPart` 提升为公开
  函数 `SafePathPart`,并新增 `SpilloverPath(stateRoot, sessionID, toolUseID)`
  作为约定路径的单一来源,`LimitResult` 改为复用该函数
- fs.Options 新增 `OutputRoot string` 字段:专门表示 `<state>/tool_outputs`
  根(与现有 `StateRoot` 字段语义重叠,后者历史上也存这个值;本次保留
  `StateRoot` 兼容性,但新工具一律读 `OutputRoot`,缺失时回落到 `StateRoot`)
- `fs.Register` 在 `OutputRoot` 非空时注册 `tool_result`,否则跳过(避免在
  无法定位 spillover 文件时给出一个 100% 失败的工具)

## Capabilities

### Modified Capabilities

- `fs-tools`:新增 `tool_result` 工具规格;`fs.Register` 在具备 spillover 配
  置时注册的工具数从 6 个变为 7 个

## Impact

- 新增 `internal/tool/ctx.go`(SessionID context helpers)及单测
- 新增 `internal/tool/fs/tool_result.go` + `tool_result_test.go`
- 修改 `internal/tooloutput/tooloutput.go`:导出 `SafePathPart` 与 `SpilloverPath`
- 修改 `internal/tool/fs/register.go`:`Options.OutputRoot` 字段 + 条件注册
- 修改 `internal/agent/agent.go` `runTool`:注入 sessionID
- 修改 `internal/cli/root.go`:把 `outputRoot` 透过新的 `Options.OutputRoot`
  字段传入 fs.Register(同时保留旧的 `StateRoot` 兼容字段)
- 不引入新依赖
- 不改 rollout / provider / config schema
