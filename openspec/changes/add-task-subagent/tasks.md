## 1. context 助手

- [x] 1.1 `internal/tool/ctx.go`:
  - `SubagentRunner` 接口:`RunSubagent(ctx, prompt string, maxTurns int) (string, error)`
  - `WithSubagentRunner` / `SubagentRunnerFromContext`
  - `WithSubagentDepth(ctx, n) ctx` / `SubagentDepthFromContext(ctx) int`
- [x] 1.2 `internal/tool/ctx_test.go`:round-trip 测试

## 2. task 工具

- [x] 2.1 `internal/tool/task/task.go`:
  - args:`{prompt string, max_turns int?}`
  - 校验非空 prompt;`SubagentRunner` 缺失返回错误;深度 ≥ 1 时拒绝递归
  - Execute:`runner.RunSubagent(ctx + depth+1, prompt, max_turns)`,把返回 string 包成 Result.Content
- [x] 2.2 `task_test.go`:happy path(用 fake runner)、缺 runner 拒绝、递归拒绝、空 prompt 拒绝

## 3. 注册

- [x] 3.1 `internal/tool/task/register.go`:`Register(reg)`
- [x] 3.2 task tool 不依赖 workspace 路径,所以注册函数签名只接 reg

## 4. agent 集成

- [x] 4.1 `agent.Options.SubagentRunner tool.SubagentRunner` 字段
- [x] 4.2 `agent.runTool`:`ctx = tool.WithSubagentRunner(ctx, a.subagentRunner)`(只有 runner 非 nil 时;depth 不在这里设,task tool 自己 +1)

## 5. cli 接线

- [x] 5.1 `cli/root.go` / `tui.go`:构造一个 `cliSubagentRunner` 类型,实现 `tool.SubagentRunner`;capture provider/tools/model/runtime;`RunSubagent` 创建 child Agent(ModeAuto, 无 EventSink, 无 rollout)
- [x] 5.2 `agent.New` 调用补 `SubagentRunner` 字段
- [x] 5.3 task.Register 加入 newToolRuntime 链

## 6. 文档

- [x] 6.1 `docs/usage.md` 加 Subagents 小节(讲深度限制 + 复用主 provider 的事实 + ApprovalAgent 依赖)
- [x] 6.2 `docs/design.md` 在工具列表与 agent loop 处补 subagent
- [x] 6.3 `openspec/changes/add-task-subagent/specs/task-subagent/spec.md`

## 7. 验证

- [x] 7.1 `go test ./...`
- [x] 7.2 `make lint`
- [x] 7.3 `make build`
