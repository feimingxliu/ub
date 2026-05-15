## Why

I-23 的 TUI 已经能运行 Agent，但 exec 风险工具在没有交互审批 UI 时只能被拒绝。I-24 需要接入权限弹窗，让危险操作能由用户显式决策，并覆盖五种 permission decision。

## What Changes

- 新增 TUI permission bridge，把 `permission.Asker` 请求转为 Bubble Tea modal 交互。
- 新增权限 modal，展示工具名、风险等级、参数摘要、preview summary 和可折叠 unified diff。
- 支持按 `1` 到 `5` 返回 allow once、deny、always command、always tool、always global。
- Plan 模式下 exec 审批显示副作用警告；agent-approve 回退人工时展示 approval agent reason。
- TUI runner 使用该 bridge 作为 permission manager 的 human Asker。
- 增加 modal/model 单测覆盖五种按键、diff 展开和请求响应。

## Capabilities

### New Capabilities

- 无。

### Modified Capabilities

- `tui-shell`: 增加阻塞式权限弹窗与 TUI permission bridge。
- `permission-manager`: human Asker 请求携带 approval agent 回退原因，供 UI 展示。

## Impact

- 修改 `internal/tui`，增加 permission bridge 和 modal 状态处理。
- 新增 `internal/tui/dialog/permission` 组件。
- 修改 `internal/cli` TUI runner，从拒绝式 asker 切换到 TUI bridge。
- 扩展 `internal/permission.Request` 字段，用于传递 approval agent reason。
