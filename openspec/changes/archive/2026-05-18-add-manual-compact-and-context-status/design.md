## Context

当前 Sprint 4 已实现 `internal/context.Estimate`、Agent 自动 summary 和 rollout `Summary` 事件。自动 summary 只在 provider 声明最大上下文且估算比例超过阈值时触发；TUI 状态栏目前显示 model、effort、mode、state、turn 和 cwd，但没有上下文使用量；slash 命令集中也没有主动压缩入口。

## Goals / Non-Goals

**Goals:**

- 在 TUI 中提供 `/compact`，让用户主动压缩当前 session 的早期上下文。
- 复用现有 summary provider、summary prompt、`keep_recent_turns` 和 rollout `Summary` 事件，避免引入第二套压缩语义。
- 在 Agent 每次准备请求和手动 compact 后上报 token 估算、最大上下文和比例，让 TUI 状态栏展示 context/token 使用量。
- 保持 headless `ub run` 行为不变。

**Non-Goals:**

- 不新增摘要重组、摘要编辑或多级摘要策略。
- 不保证估算值等于 provider 实际 usage；状态栏显示的是请求前估算值和 provider caps。
- 不把 `/compact` 作为普通 prompt 发送给 Agent。

## Decisions

1. `/compact` 走 TUI runner 扩展接口。
   - 方案：新增可选 `CompactRunner` 接口，TUI 只有在 runner 支持时才执行压缩。
   - 原因：slash 命令属于本地控制面，不应进入 Agent prompt；可选接口避免破坏测试 runner 和未来非 Agent runner。
   - 备选：把 `/compact` 转成特殊 prompt。该方案会污染对话历史，且无法保证一定触发 summary。

2. Agent 暴露手动 compact 方法，复用 `prepareMessages` 的 summary 子流程。
   - 方案：抽出共同的 `compactMessages` 逻辑；自动 summary 仍先判断阈值，手动 compact 跳过阈值但仍要求存在可压缩前缀。
   - 原因：自动和手动行为保持同一 rollout 格式、同一 summary message 结构和同一最近 turn 保留规则。
   - 备选：在 TUI runner 中直接调用 summary provider。该方案会复制 Agent 私有逻辑，容易与自动 summary 漂移。

3. 上下文用量通过 Agent/TUI 事件传播。
   - 方案：在 runtime event 上增加 `ContextUsedTokens`、`ContextMaxTokens`、`ContextRatio` 字段；TUI 收到任何携带该字段的事件就更新状态栏。
   - 原因：状态栏更新不需要额外持久化事件，也不影响 provider stream 语义。
   - 备选：从 TUI 直接重新估算 runner history。该方案会把 token 估算耦合到 TUI，并且无法准确知道 Agent 实际请求消息是否已被 summary 替换。

## Risks / Trade-offs

- 手动 compact 在历史轮次不足时可能没有效果 -> TUI 显示明确的本地提示，不生成错误 turn。
- 状态栏宽度有限 -> 使用紧凑格式 `ctx: used/max pct`，并沿用现有状态栏收缩逻辑。
- provider 未声明最大上下文时无法计算比例 -> 状态栏仍显示 used token，省略 max 和百分比。
