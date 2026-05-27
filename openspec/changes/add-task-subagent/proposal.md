## Why

`roadmap-v2.md` §3-03 把 subagents 列为 V2 主体 L 项,但明确写了"依赖 §4-01
agent loop 解耦先做"。§4-01 尚未启动。本 change 是 S3-03 的**最小可用版本**
—— 在不动 loop 的前提下让用户体验到"派发子 agent 跑调研任务"这一核心
价值,并给后续完整版铺路。

最小版的取舍:

- 同进程跑子 agent,**复用主 agent 的 provider 与 tool registry**
- 子 agent 不再创建自己的子 agent(深度上限 = 1),避免递归把 token 经济
  搅到不可分析
- TUI 多 pane 渲染留到 §4-02 事件总线落地之后;本版只是子 agent 跑完
  把最终文本作为 `task` tool 的 Result.Content 回灌主 agent

复用 provider/tools 的代价是子 agent 不能用独立模型 / 独立工具集 —— 这是
roadmap "完整版"的目标,但需要先把 agent loop 解耦清楚。最小版先把"
派发 + 返回 + 独立 context"三件事跑通,价值已经成立。

## What Changes

- 新建 `internal/tool/task/`:
  - `task(prompt, max_turns?)`:`Risk=safe`(子 agent 自己的 tool 调用各自走
    自己的 permission;`task` tool 本身只是分派)
  - 通过 ctx 上注入的 `SubagentRunner`(在 `internal/tool` 包定义的接口)
    跑子 agent;ctx 已被 `task` tool 标记 `depth=1`,再次嵌套 task 拒绝
- `internal/tool/ctx.go` 新增 `WithSubagentRunner(ctx, runner)` / 
  `SubagentRunnerFromContext(ctx)`,以及 `WithSubagentDepth` / 
  `SubagentDepthFromContext`(int);depth 默认 0,task 调用时设到 +1
- `internal/agent`:新增 `SubagentRunner` 实现适配器:`Options.SubagentRunner`
  字段;agent.runTool 在调用前把 `ctx = tool.WithSubagentRunner(ctx, a.subRunner)` 
- `internal/cli/root.go` 与 `tui.go`:为主 agent 注入一个 SubagentRunner 实现,
  它 capture 当前 provider / tools / model / runtime,创建 child Agent 跑
  单 prompt;child agent 的 SessionID = `<parent>__sub_<ulid>`,**不**写
  rollout(子 agent rollout 暂不持久化,避免 store 大小爆炸 —— 后续 change
  再补)
- 子 agent 不要 EventSink(避免事件流叠交);它的输出由 `task` tool 直接
  返回
- 子 agent 不要 permission manager 的人审批,**但保留 Risk gate**:子 agent
  里的 RiskExec 工具(bash 等)行为视 mode 而定 —— work 模式下 child agent
  没 human asker,permission.Ask 会失败 → 设计上 child agent 走 ModeAuto,
  靠 ApprovalAgent;主 agent 没配 ApprovalAgent 时报错给 task tool

## Capabilities

### New Capabilities

- `task-subagent`:`task` tool schema、子 agent 派发协议、depth 上限、ctx
  传递的 SubagentRunner 接口

## Impact

- 新增 `internal/tool/task/` 包及测试
- 修改 `internal/tool/ctx.go` 与 `ctx_test.go` 加 SubagentRunner / Depth 助手
- 修改 `internal/agent/agent.go`:`Options.SubagentRunner` 字段,`runTool` 注入 ctx
- 修改 `internal/cli/root.go` 与 `tui.go`:构造 runner 并注册 task tool
- 不引入新依赖
- 不改 rollout schema
- breaking change:无;只在用户配了 approval_agent(`auto` 用)时 task 才会
  真正跑通有 RiskExec 工具的子任务
