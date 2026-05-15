## Context

当前 TUI 输入框只区分普通 prompt 和权限 modal 按键。路线图 I-26 要求基础 slash 命令，但不要求补全、自定义 alias 或复杂配置热加载。实现应保持命令解析独立可测，并让可立即生效的命令影响后续 Agent turn。

## Goals / Non-Goals

**Goals:**

- 支持 `/model`、`/mode`、`/clear`、`/sessions`、`/help`、`/quit`、`/config`、`/profile`。
- 输入以 `/` 开头时在本地执行，不发送给 Agent。
- `/model <id>` 和 `/mode <mode>` 更新状态栏，并同步到 runner。
- `/clear` 清空消息列表；`/quit` 退出；其他命令输出简短状态或操作提示。
- 单测覆盖 parser 和 model 执行路径。

**Non-Goals:**

- 不实现命令补全、自定义 alias 或配置热加载。
- 不在 `/sessions` 内做交互式 session resume；I-33 处理 resume。
- 不在 `/profile` 内重新加载完整配置；V1 改配置仍需要重启。

## Decisions

1. **独立 parser。** 新增 `internal/tui/slash`，解析命令名和参数，返回结构化 Command。这样命令语法不依赖 Bubble Tea，可直接单测。

2. **TUI model 执行本地命令。** `/clear`、`/help`、`/config`、`/sessions` 等只影响消息区或状态栏，不进入 Agent loop。

3. **Runner 控制接口可选。** 定义 `ControlRunner` 扩展接口，支持 `SetModel` 和 `SetMode`。fake runner 可不实现；CLI runner 实现后影响后续 Agent 请求。

4. **Profile 命令先做状态提示。** `/profile <name>` 显示如何用 `--profile`/`UB_PROFILE` 重启，避免在 I-26 引入运行时 config merge 与 provider 重建。

## Risks / Trade-offs

- **slash 命令和普通 prompt 冲突** → 以 `/` 开头的输入一律按命令解析；用户要发字面 slash prompt 可后续增加 escape。
- **`/profile` 不热切换** → 与 requirements 中 V1 配置修改需重启一致，避免半切换 provider/config。
- **状态消息污染对话区** → 使用 `System` 角色显示本地反馈，后续可改成独立通知栏。
