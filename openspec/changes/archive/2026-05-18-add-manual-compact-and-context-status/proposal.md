## Why

Sprint 4 已经具备 token 估算和自动 summary，但用户在长会话中还需要主动压缩上下文的入口，并且需要在 TUI 中直接看到当前上下文使用量，避免只能等自动阈值触发。

## What Changes

- 增加 TUI 本地 slash 命令 `/compact`，用于主动触发当前 session 的上下文压缩。
- 手动压缩复用现有 summary prompt、small model、rollout Summary 事件和“保留最近 N 个 user turn”的策略，但不依赖自动触发阈值。
- Agent 向 TUI 上报当前请求的 token 估算值、provider 最大上下文和使用比例。
- TUI 状态栏新增 context/token 使用量展示，并在普通 turn 和 `/compact` 后更新。
- 更新文档中 Sprint 4/TUI 命令范围，使 `/compact` 和 context status 成为明确 V1 行为。

## Capabilities

### New Capabilities

- 无

### Modified Capabilities

- `context-management`：增加手动 compact 触发和上下文使用量上报要求。
- `tui-shell`：状态栏增加 context/token 使用量展示。
- `tui-slash-commands`：支持 `/compact` 本地命令并触发 runner 压缩。

## Impact

- 影响 `internal/agent` 的 summary 准备逻辑、手动压缩入口和运行事件。
- 影响 `internal/tui` 的 slash 命令、runner 接口、状态栏渲染和相关测试。
- 影响 `internal/cli` 的 TUI runner 与 Agent 事件转换。
- 更新 `docs/requirements.md`、`docs/design.md` 和 `docs/roadmap.md` 中的 TUI/上下文说明。
