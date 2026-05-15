## Why

TUI 已经具备对话、权限和 diff 能力，但常用操作仍需要退出到 CLI。I-26 增加基础 slash 命令，让用户能在交互界面内完成清屏、查看帮助、切换模型/模式等高频动作。

## What Changes

- 新增 TUI slash command parser，覆盖 `/model`、`/mode`、`/clear`、`/sessions`、`/help`、`/quit`、`/config`、`/profile`。
- TUI 在输入以 `/` 开头时执行命令，不发送给 Agent。
- `/model <id>` 和 `/mode <mode>` 更新状态栏，并同步到支持控制的 runner。
- `/clear` 清空消息列表；`/quit` 退出 TUI；其他命令输出当前状态或明确提示。
- 增加 parser 和 model 单测。

## Capabilities

### New Capabilities

- `tui-slash-commands`: TUI 内置 slash 命令解析和执行行为。

### Modified Capabilities

- `tui-shell`: 输入框识别 slash 命令并在本地执行。

## Impact

- 新增 `internal/tui/slash` 包或等价 parser。
- 扩展 `internal/tui` model 和 runner 控制接口。
- 修改 CLI TUI runner，使 `/model`、`/mode` 可影响后续 Agent turn。
