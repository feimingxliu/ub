## 1. config schema

- [x] 1.1 `internal/config/types.go`:新增 `HooksConfig`、`HookList`(`map[string][]HookSpec`?)、`HookSpec` 字段:
  - `Command []string`(必填,argv;空 → 跳过该 hook)
  - `Tools []string`(空 = all)
  - `Timeout time.Duration`(0 → 默认 10s,上限 60s 由 hook 包钳制)
  - `OnFailure string`(`warn` 默认 / `block`)
  - `Env []string`(白名单 key,默认空)
- [x] 1.2 `Config.Hooks HooksConfig` 字段(yaml/json tag);merge 函数补到 `merge.go`(append 切片而非覆盖)
- [x] 1.3 `merge_test.go` 补:project 层 hook 列表会追加到 user 层而不是替换

## 2. hook 包

- [x] 2.1 `internal/hook/event.go`:`Event` struct(Kind、SessionID、Turn、ToolName、ToolUseID、ToolArgs json.RawMessage、Result *tool.Result)
- [x] 2.2 `internal/hook/runner.go`:`Runner` 接口 + `shellRunner` 实现
  - `Pre(ctx, Event) (Decision, error)`:仅返回 block 信号
  - `Post(ctx, Event)`:fire-and-forget 语义,内部 goroutine 跑完即丢
  - 共用 `runOne(ctx, HookSpec, Event) (Outcome, error)`:argv 启动、stdin 写 JSON、env 白名单、ContextWithTimeout、读 stdout/stderr 各最多 4KB
- [x] 2.3 `internal/hook/runner_test.go`:
  - happy:命令成功不 block
  - timeout:5ms 超时被 kill,返回 err
  - block 策略:exit code 非 0 时 Decision.Block = true
  - tools filter:命中 / 未命中
  - env 白名单:未列出的 env 不出现在子进程

## 3. agent 集成

- [x] 3.1 `Options.Hooks hook.Runner`(可空,空时 agent 不调任何 hook)
- [x] 3.2 `agent.Run`:首次 provider 请求前 `Hooks.Pre(ctx, hook.Event{Kind:"pre_user_turn"})`;loop 结束(defer)`Hooks.Post(ctx, hook.Event{Kind:"post_user_turn"})`
- [x] 3.3 `agent.runTool`:permission 之前 `Pre("pre_tool_call")`,如果 Block 则跳过 Execute 并回灌 IsError result;Execute 完 `Post("post_tool_call")`
- [x] 3.4 hook 触发也通过 `emitActivity{kind=hook}` 让 TUI 看见
- [x] 3.5 agent_test:写一个 fake Runner 验证 4 个触发点被调用 + block 决定生效

## 4. cli 接线

- [x] 4.1 `cli/root.go`:在 `newAgent` / `newChatSession` 路径,从 `cfg.Hooks` 构造 `hook.NewShellRunner(cfg.Hooks)`,塞进 `Options.Hooks`
- [x] 4.2 cfg.Hooks 为空时 NewShellRunner 返回 noop runner(避免无谓 goroutine 开销)

## 5. 文档

- [x] 5.1 `docs/usage.md`:加 Hooks 章节,讲配置样例 + 行为约定 + 失败语义
- [x] 5.2 `docs/design.md`:在 agent loop / tool 流程图旁加 hook 触发点位置
- [x] 5.3 `openspec/changes/add-hooks/specs/hooks/spec.md`:Requirements + Scenarios
- [x] 5.4 `make schema` 重新生成 `schema/config.schema.json`

## 6. 验证

- [x] 6.1 `go test ./...`
- [x] 6.2 `make lint`
- [x] 6.3 `make build`
- [x] 6.4 手测:在 .ub/config.yaml 配一个 echo hook 跑 ub,确认 stderr 出现在 activity
