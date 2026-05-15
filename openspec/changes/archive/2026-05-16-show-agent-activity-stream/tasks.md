## 1. Provider Reasoning Events

- [x] 1.1 在 `internal/provider` 中新增 reasoning/thinking event type 与字段，并保持现有事件兼容。
- [x] 1.2 扩展 fake provider 配置和 Go helper，支持脚本化 reasoning 事件。
- [x] 1.3 在支持 reasoning 字段的 provider adapter 中透传可展示 reasoning delta；不支持的 adapter 保持无 reasoning 事件。
- [x] 1.4 为 provider reasoning 事件、fake 脚本和不伪造行为补单元测试。

## 2. Agent Activity Events

- [x] 2.1 扩展 Agent event 模型，新增结构化 activity payload 与 activity kind。
- [x] 2.2 将 provider reasoning 事件转换为 `thinking` activity，且不混入 assistant 正文。
- [x] 2.3 在 tool dispatch 周期输出工具 lifecycle activity，包含工具名、状态、安全输入摘要和结果摘要。
- [x] 2.4 为权限审批输出 activity，覆盖 approval agent allow/deny/unsure/error 与 human fallback 决策。
- [x] 2.5 实现工具参数摘要的白名单、长度限制和 secret 遮蔽。
- [x] 2.6 补充 Agent 单元测试，覆盖 thinking、工具生命周期、权限活动和无回调兼容行为。

## 3. TUI Activity Rendering

- [x] 3.1 扩展 TUI runner 事件转换，把 Agent activity 传入 TUI model。
- [x] 3.2 在消息列表中新增 activity 渲染样式，与用户消息和 assistant 正文区分。
- [x] 3.3 将 thinking、工具状态、审批结果和错误活动渲染为紧凑摘要。
- [x] 3.4 确保活动消息参与现有宽度换行和 PageUp/PageDown/鼠标滚动。
- [x] 3.5 为长摘要、secret 遮蔽、工具生命周期和审批结果渲染补 TUI 单元测试。

## 4. Documentation and Validation

- [x] 4.1 更新 `docs/requirements.md`、`docs/design.md` 和相关 OpenSpec 主 specs，说明活动流展示边界。
- [x] 4.2 运行 `go test ./internal/provider/... ./internal/agent ./internal/tui ./internal/cli`。
- [x] 4.3 运行 `go test ./...` 与 `git diff --check`。
- [x] 4.4 手动用 fake provider 脚本验证 TUI 展示 thinking、工具调用和权限活动。
