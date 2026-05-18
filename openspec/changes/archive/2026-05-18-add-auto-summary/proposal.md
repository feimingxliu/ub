## Why

I-27 已经提供 token 估算，但长 session 仍会把完整历史发给 provider，最终超过模型上下文窗口。I-28 需要在 agent 发请求前自动压缩早期历史，让长会话可以继续运行。

## What Changes

- Agent 发请求前根据 `Estimate(messages, model) / provider.Caps().MaxContextTokens` 与 `context.trigger_ratio` 判断是否触发 summary
- 触发时使用 `small_model`（未配置则回退当前模型）和内嵌 summary prompt，把早期历史压缩成单条 system 摘要
- 保留最近 `context.keep_recent_turns` 轮历史，默认 3 轮
- rollout 新增 `summary` 事件，记录摘要文本和被压缩的消息范围；恢复历史时 summary 事件会还原为 system 消息
- usage 事件回灌 I-27 的估算校正接口
- 覆盖自动触发、未触发、历史替换和 rollout summary 事件的单元测试

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `context-management`: 增加自动 summary 触发、压缩和 usage 校正行为
- `agent-loop`: Agent 请求前接入上下文压缩，确保 provider 收到压缩后的历史
- `rollout-events`: 增加 summary 事件类型和历史恢复语义

## Impact

- 影响 `internal/agent` 的请求准备和 usage 消费路径
- 影响 `internal/rollout` 的事件类型、payload helper 和历史恢复
- 影响 CLI/TUI agent runner 的 summary provider/model/config 注入
- 新增 summary prompt 模板，使用 `embed` 随 binary 发布
