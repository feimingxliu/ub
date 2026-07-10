## Why

ub 当前只在模型配置和 provider caps 之间选择静态 `MaxContextTokens`。对未知模型或 OpenAI-compatible 自定义端点，这个值可能缺失或与真实后端不一致，导致自动 compact 过早、过晚，甚至只能在 provider overflow 后被动恢复。

S4-06 已提供稳定 prompt section 边界，v0.5 的下一步需要先建立可解释、可学习的上下文窗口解析能力，才能让后续 S4-08 ContextDecision 基于可信窗口做决策。

## What Changes

- 新增统一的 `ContextWindowResolver`，返回有效窗口大小、来源和置信度。
- 保持显式模型配置最高优先级；没有显式配置时综合模型元信息、provider caps 与历史观察。
- 按 provider、规范化 endpoint 和 model 隔离缓存成功 usage 与 context overflow 观察。
- 从带数值的 overflow 错误学习真实窗口；无法提取数值时用本次请求估算形成保守上界，并避免与已成功 usage 冲突。
- Agent 的自动 summary、上下文用量事件和 TUI context ratio 统一使用 resolver 结果；provider usage 和 overflow 自动回灌 resolver。
- 缓存属于可丢弃派生状态；读取失败时安全回退到静态候选，不阻断 Agent 启动或请求。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `context-management`: 增加上下文窗口来源解析、历史观察缓存、运行时回灌和安全降级要求。

## Impact

- 主要影响 `internal/tokenizer` 附近的窗口解析逻辑、`internal/agent` 的 summary/usage/overflow 路径，以及 CLI 创建 Agent 时的 provider/model 元数据接线。
- 在 `$XDG_STATE_HOME/ub` 下新增可丢弃的 context-window 派生状态文件，不新增用户配置字段，不需要 config schema 迁移。
- 不改变 provider 请求协议、summary prompt、tool schema 或 prompt inspect 的无 provider 副作用契约。
