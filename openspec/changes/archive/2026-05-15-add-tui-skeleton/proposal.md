## Why

Sprint 3 需要先建立可运行的终端交互外壳，让后续流式 Agent、权限弹窗和 slash 命令有稳定承载点。I-22 只打通最小 TUI 体验，避免在接入 Agent 前引入过多交互复杂度。

## What Changes

- 新增 Bubble Tea TUI 根 model，包含消息列表、输入框和状态栏三个基础组件。
- 启动 `ub` 不带子命令时进入 TUI。
- 用户输入普通文本并按 Enter 后，消息会回显到聊天区。
- 支持 Ctrl+C 退出 TUI。
- 增加最小 teatest/模型单元测试覆盖输入回显与退出。

## Capabilities

### New Capabilities

- `tui-shell`: 终端聊天 UI 的基础布局、输入回显和退出行为。

### Modified Capabilities

- 无。

## Impact

- 新增 `internal/tui/` 包及基础组件。
- 调整 `internal/cli` 根命令默认行为：无子命令时启动 TUI。
- 新增 Bubble Tea/Bubbles 相关依赖与测试。
