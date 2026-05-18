## Context

I-27 已经提供 `internal/context.Estimate` 和 usage 校正入口。当前 `agent.Run` 会把 `req.History + userMsg` 原样传给 provider，并在 usage 事件里只写 rollout，不会在请求前压缩历史。I-28 需要把 docs/design 中的 ctx prepare 步骤落到现有 agent loop、CLI/TUI runner 和 rollout 事件模型里。

## Goals / Non-Goals

**Goals:**

- 在 agent 每次 provider 请求前检查 token 比例，超过阈值时自动 summary。
- 使用可配置的 `small_model` 执行 summary prompt；未配置时回退当前模型。
- 把早期历史替换为单条 system 摘要，保留最近 `keep_recent_turns` 个 user turn 及其后续消息。
- 把 summary 写入 rollout，并让 session resume 能把 summary 恢复为 system message。
- 在收到 provider usage 时调用 I-27 的 `ObserveUsage` 校正后续估算。

**Non-Goals:**

- 不实现用户手动 `/summarize`。
- 不实现多段摘要重组或摘要质量评分。
- 不引入独立 summary provider 配置；本次复用当前 provider，并用 `small_model` 选择模型。

## Decisions

1. Agent 增加 summary 相关 options，而不是让 CLI/TUI 在外部预处理历史。这样 `ub run`、TUI 和未来 resume 路径复用同一套行为，也能在 tool loop 的每轮 provider 调用前重新检查。

2. 最近历史按 user turn 计算：从消息尾部向前找到第 `keep_recent_turns` 个 user 消息，保留该消息到末尾，压缩更早消息。这样一个 turn 内的 assistant/tool_result 不会被拆散。

3. summary provider 默认使用当前 provider 的新实例，summary model 默认 `cfg.small_model`，为空时使用当前模型。为 fake provider 和真实 provider 都避免与主请求共享 stream/call 状态。

4. summary prompt 使用 `embed` 内置模板，输入为 provider-neutral 文本渲染，不暴露 provider SDK 结构。summary 返回只收集 text delta，忽略 reasoning delta，tool_call 或 error 视为 summary 失败并返回可读错误。

5. rollout 新增 `summary` event 和 payload：保存 summary 文本、被压缩消息数量、保留消息数量与估算 token。`MessageFromEvent` 把它恢复为 system message，使 session resume 后继续使用压缩历史。

## Risks / Trade-offs

- [Risk] summary provider 调用失败会阻塞本轮请求。→ 返回错误并写入现有 error rollout，避免静默丢历史。
- [Risk] summary 本身可能丢失细节。→ 默认保留最近 3 个 user turn，且本次不做多段重组，减少早期复杂度。
- [Risk] max context 为 0 或 provider 未声明窗口时无法判断。→ 不触发 summary，保持现有行为。
- [Risk] small_model 与当前 provider 不兼容。→ CLI/TUI 使用当前 provider 解析模型；配置错误会在 provider 调用或模型选择阶段暴露。
