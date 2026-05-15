## Context

I-22 的 TUI 只在本地回显用户输入；现有 Agent loop 已经支持 provider 流、工具调用、权限与 rollout，但它只向调用方返回最终文本。TUI 需要在 Agent 运行过程中持续接收事件，才能边生成边渲染，并展示工具调用状态。

## Goals / Non-Goals

**Goals:**

- Agent 提供可选事件回调，不影响 `ub run` 的 headless 行为。
- TUI 通过 Runner 接口启动 Agent，通过 channel 接收流式事件。
- 消息列表能追加 assistant 文本 delta，并显示工具调用开始/结束。
- 状态栏显示 model、execution mode 和当前 turn。
- `ub` 默认 TUI 使用真实配置路径创建 provider、工具 registry、permission manager 和 rollout session。

**Non-Goals:**

- 不实现权限 modal；本迭代的 TUI runner 对需要人工审批的 exec 调用先拒绝，I-24 替换为交互审批。
- 不实现 slash 命令、diffview 或 session resume。
- 不改变 provider 协议或 tool schema。

## Decisions

1. **Agent 事件回调放在 `agent.Options`。** 新增 `EventSink func(Event)`，在文本 delta、tool call、tool result、done/error 时调用。备选方案是在 TUI 里重新消费 provider stream，但会复制 Agent 的工具调度和 rollout 逻辑。

2. **TUI 定义 Runner 接口。** `internal/tui` 不直接依赖 config/provider/store，只依赖 `Runner.Run(ctx, prompt, events)`。CLI 侧适配现有 runtime，测试可注入 fake runner。

3. **Bubble Tea channel 采用“一次等一条消息”。** 发送 prompt 后返回一个等待 channel 的 `tea.Cmd`；每收到一个事件就更新 model 并继续等待，直到 Done/Error。这样避免 goroutine 直接改 model 状态。

4. **每次 TUI 输入对应一个 turn。** Runner 维护 session state、history 和 turn 计数；TUI 只展示当前 turn。后续 I-33 再处理 resume。

## Risks / Trade-offs

- **Agent 回调阻塞会拖慢 provider stream** → CLI runner 使用带缓冲 channel，并在 context 取消时停止发送。
- **I-23 未实现权限弹窗** → TUI runner 的人工审批路径先拒绝 exec，避免静默执行危险命令；I-24 会接入真实 Asker。
- **状态栏 token/context 暂无数据** → 本迭代只显示 roadmap 明确要求的 model、execution mode 和 turn。
