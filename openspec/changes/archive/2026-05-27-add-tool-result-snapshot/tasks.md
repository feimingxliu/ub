## 1. session-id context helpers

- [x] 1.1 在 `internal/tool/ctx.go` 增加 `WithSessionID(ctx, sid) context.Context` 与 `SessionIDFromContext(ctx) string`(独立 key 类型避免碰撞)
- [x] 1.2 在 `internal/tool/ctx_test.go` 补 unit test:写入后能读出;未写入返回空串;空串写入不写 ctx

## 2. tooloutput 公开化

- [x] 2.1 把 `safePathPart` 重命名为可导出的 `SafePathPart`,调用点全部更新
- [x] 2.2 新增 `SpilloverPath(stateRoot, sessionID, toolUseID string) (string, error)`,基于 `OutputRoot` + `SafePathPart` 拼接;`LimitResult` 与 `writeSpillover` 共用此函数
- [x] 2.3 单测覆盖 `SpilloverPath` 的 happy path 与默认 stateRoot 行为

## 3. agent 注入 sessionID

- [x] 3.1 `internal/agent/agent.go` `runTool`(或更早的 `executeCall`):在调用 `tl.Execute(ctx, args)` 之前 `ctx = tool.WithSessionID(ctx, sessionID)`
- [x] 3.2 agent 单测断言:经过 runTool 后的 ctx 携带 sessionID(可写一个观测型 tool)

## 4. tool_result 工具

- [x] 4.1 `internal/tool/fs/tool_result.go`:
  - args:`{tool_use_id: string(必填), offset?: int, limit?: int}`
  - 从 ctx 拿 sessionID,空则返回错误
  - 通过 `tooloutput.SpilloverPath` 计算路径
  - 不存在则返回错误说明 "tool_use_id not found or output was not spilled"
  - 存在则按 `read` 工具同样的"带行号 + offset/limit + 默认上限"格式返回内容
- [x] 4.2 与 `read` 共用 `formatLines` 风格的格式化函数(若可复用)
- [x] 4.3 `tool_result_test.go`:happy path、缺失文件错误、ctx 无 sessionID 错误、offset/limit、超大文件截断

## 5. 注册

- [x] 5.1 `fs.Options` 添加 `OutputRoot string`(语义更清晰);保留 `StateRoot` 兼容,当 `OutputRoot` 空时回落
- [x] 5.2 `fs.RegisterWithOptions` 在 `OutputRoot != ""` 时注册 `tool_result`
- [x] 5.3 更新 `register_test.go`:6 件套 + 当 OutputRoot 提供时 7 件套
- [x] 5.4 `internal/cli/root.go` 把 outputRoot 也填到 `Options.OutputRoot`

## 6. 文档

- [x] 6.1 `docs/design.md`:在 fs 工具集合处补 `tool_result` 描述
- [x] 6.2 `openspec/changes/add-tool-result-snapshot/specs/fs-tools/spec.md`:补 `tool_result` Requirement 与 Scenarios

## 7. 验证

- [x] 7.1 `go test ./...`
- [x] 7.2 `make lint`
- [x] 7.3 `make build`
