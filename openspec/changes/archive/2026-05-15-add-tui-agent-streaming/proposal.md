## Why

I-22 已经提供本地回显式 TUI，但还不能执行真实 Agent。I-23 需要把 TUI 输入接入现有 Agent loop，并把 provider 的流式输出逐步渲染到屏幕，为权限弹窗和 diff 预览提供真实运行路径。

## What Changes

- 为 Agent 增加可选事件回调，向调用方报告文本增量、工具调用开始/结束和完成状态。
- 在 TUI 中加入 Runner/channel 桥接：用户发送消息后启动 Agent，并按事件流更新消息列表和状态栏。
- 状态栏显示当前 model、execution mode 和 turn。
- `ub` 默认 TUI 使用真实配置、provider、工具 registry、rollout/session 存储运行 Agent。
- 增加 fake runner/model 单测，验证流式文本追加和完成状态。

## Capabilities

### New Capabilities

- 无。

### Modified Capabilities

- `tui-shell`: TUI 从本地回显升级为可调用 Agent 并流式渲染结果。
- `agent-loop`: Agent loop 对调用方暴露可选运行事件，不改变 headless `ub run` 行为。

## Impact

- 修改 `internal/agent` 增加事件类型和可选回调。
- 扩展 `internal/tui` model，新增 Runner 接口和 channel 消费逻辑。
- 修改 `internal/cli` 根命令启动路径，创建 TUI runner 并复用现有 provider/tool/rollout 组装逻辑。
