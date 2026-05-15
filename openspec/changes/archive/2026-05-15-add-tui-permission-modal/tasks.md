## 1. Permission 数据

- [x] 1.1 扩展 `permission.Request`，加入 approval agent 回退原因字段。
- [x] 1.2 在 `permission.Manager` 的 agent-approve 回退 human 路径填充该原因。
- [x] 1.3 添加 permission manager 单测覆盖 reason 透传。

## 2. TUI Bridge 与 Modal

- [x] 2.1 实现 `tui.PermissionBridge`，满足 `permission.Asker` 并通过 channel 等待 TUI 决策。
- [x] 2.2 新增 `internal/tui/dialog/permission` modal 组件，渲染工具、risk、mode、args、preview summary、diff 和上下文提示。
- [x] 2.3 实现 `1`-`5` 决策按键映射和 `d` 展开/折叠 diff。
- [x] 2.4 TUI root model 接收 permission request，modal 打开时优先处理审批按键并回传 decision。

## 3. CLI 接入

- [x] 3.1 root TUI 启动时创建 permission bridge，并传给 TUI model 与 TUI runner。
- [x] 3.2 TUI runner 使用 bridge 作为 permission manager 的 Asker，替换 I-23 的 deny asker。

## 4. 验证

- [x] 4.1 添加 modal 单测覆盖五个按键、Plan exec 警告、approval reason 和 diff 展开。
- [x] 4.2 添加 TUI model 单测覆盖 permission request 到 response 的完整按键流。
- [x] 4.3 运行 `go test ./...`。
- [x] 4.4 运行 `make build`。
