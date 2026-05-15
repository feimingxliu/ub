## Context

权限 Manager 已经支持五种 human decision、session/global allow-rule、黑名单和 agent-approve 回退，但 TUI runner 在 I-23 中仍使用临时拒绝式 Asker。I-24 需要把该同步审批接口桥接到 Bubble Tea 的异步消息循环。

## Goals / Non-Goals

**Goals:**

- TUI 提供阻塞式权限 modal，覆盖五种 Decision。
- permission.Asker 可从 Agent goroutine 发请求，并等待 TUI 按键响应。
- modal 显示 tool、risk、mode、args、preview summary、可折叠 unified diff。
- Plan 模式 exec 审批显示副作用警告；agent-approve 回退显示 reason。
- TUI runner 使用真实 TUI Asker，不再默认拒绝需要人工审批的 exec。

**Non-Goals:**

- 不实现富语法高亮 diff；I-25 负责升级 diffview。
- 不实现鼠标交互或自定义快捷键。
- 不改变 permission.Manager 的决策语义和规则持久化格式。

## Decisions

1. **用 bridge 而不是让 Manager 依赖 TUI。** `internal/tui.PermissionBridge` 实现 `permission.Asker`，内部用 request/response channel；Manager 不知道 Bubble Tea，TUI model 也不直接调用 Manager。

2. **modal 是 model 的阻塞状态。** `Model.Update` 在有 pending permission 时优先处理 `1`-`5` 和 `d`，普通输入暂不进入 textinput，避免用户在审批时误发送 prompt。

3. **approval reason 放进 permission.Request。** Manager 在 agent-approve 回退 human 前填充 `ApprovalReason`，UI 只负责展示。备选方案是复用 `ContextSummary`，但会混淆字段语义。

4. **AlwaysGlobal 仍由 Manager 持久化。** modal 只返回 `DecisionAlwaysGlobal`；规则写入和错误处理继续由 permission.Manager 负责，避免 UI 复制权限逻辑。

## Risks / Trade-offs

- **Agent goroutine 等待 UI 可能泄漏** → bridge 在 context 取消时返回错误，TUI 退出会取消 runner context。
- **modal 文本过长影响布局** → I-24 只做文本截断和折叠 diff，I-25 再处理富 diff。
- **global 保存提示无法确认写入完成** → 本迭代 modal 展示选择含义，实际保存仍以 Manager 返回结果为准；保存失败会作为 tool error 回灌。
