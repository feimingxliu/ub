## Why

I-24 的权限 modal 已能展示原始 unified diff，但多文件变更和代码可读性仍较弱。I-25 需要把 preview 区升级为可复用的富 diffview，为 edit/write 审批提供更清晰的变更检查界面。

## What Changes

- 新增 `internal/tui/diffview` 组件，接收 `tool.FileDiff` 列表并渲染 unified diff。
- 使用 Chroma 按文件语言高亮 diff 内容，至少覆盖 Go、Python、TypeScript 常见路径。
- 支持多文件 diff 的文件 tab 展示，并提供左右/上下切换当前文件。
- 将 I-24 permission modal 的展开 diff 改为调用 diffview。
- 添加单测覆盖多文件切换和常见语言高亮不 panic。

## Capabilities

### New Capabilities

- `tui-diffview`: 终端 unified diff 富渲染、多文件选择和语言高亮。

### Modified Capabilities

- `tui-shell`: 权限 modal 的 preview 展开区使用富 diffview 渲染。

## Impact

- 新增 `internal/tui/diffview` 包。
- 修改 `internal/tui/dialog/permission` modal。
- 新增 Chroma 依赖。
- 扩展 TUI 相关 specs 和测试。
