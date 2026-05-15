## Context

当前 CLI 已经具备 `chat`、`run`、`config`、`sessions` 等命令，但根命令仍主要服务于命令行路径。Sprint 3 需要把默认入口切换为 Bubble Tea TUI，并先提供一个不依赖 provider、agent 或权限系统的可测试 UI 外壳。

## Goals / Non-Goals

**Goals:**

- 提供 `internal/tui` 根 model 和基础组件，能渲染聊天区、输入区和状态栏。
- `ub` 无子命令启动 TUI，显式子命令行为不变。
- Enter 将当前输入作为用户消息追加到消息列表，输入框清空。
- Ctrl+C 退出程序。
- 用模型级单测覆盖输入回显、空输入忽略和退出行为。

**Non-Goals:**

- 不接入 Agent、provider 或 rollout。
- 不处理流式 token、权限弹窗、diff、slash 命令或 session resume。
- 不实现复杂主题系统；只使用当前配置中的基础主题字段预留入口。

## Decisions

1. **TUI 包保持纯应用层外壳。** `internal/tui` 暴露 `Run(ctx, Options) error` 和可测试 model 构造函数，避免 CLI 直接操作 Bubble Tea 内部状态。备选方案是把 model 写在 `internal/cli`，但会让后续 I-23/I-24 的事件桥接难以复用。

2. **组件先用轻量结构封装。** 消息列表、输入框、状态栏放在同一包内，内部可用 `bubbles/textinput` 和 `viewport`。不在 I-22 拆过细子包，等 diff/modal 出现后再按功能拆分。

3. **根命令默认进入 TUI。** cobra 无子命令时调用 TUI；`ub run`、`ub chat` 等显式命令保留当前路径。这与需求中“主界面是 TUI”一致，也不破坏已有脚本化子命令。

4. **测试优先验证 model 行为。** I-22 的关键风险是键盘事件和状态更新，因此单测直接驱动 `Update`，比快照化整屏输出更稳定。

## Risks / Trade-offs

- **新增 Bubble Tea 依赖增加构建面** → 只引入 `bubbletea` 和必要 `bubbles`，不在本迭代引入高亮或 diff 依赖。
- **默认入口变化可能影响无参数脚本** → 现有自动化应使用显式子命令；根命令进入 TUI 是产品目标。
- **终端尺寸差异导致测试脆弱** → 单测聚焦 model 状态，少量断言 View 中包含关键文本。
