## Context

ub 目前把最大上下文窗口保存在 provider-level `Caps().MaxContextTokens` 中。该模型对 OpenAI-compatible 网关不够精确，因为一个 provider 名下可能暴露多个上下文窗口不同的模型。已有 `providers.<name>.models.<model>` 配置可覆盖 reasoning 能力，适合继续承载模型级上下文窗口。

## Goals / Non-Goals

**Goals:**

- 支持在 provider model 配置中声明 `max_context_tokens`。
- Agent 的自动 summary 阈值和 context status 优先使用模型级窗口。
- 未配置模型级窗口时保持现有 provider caps 行为。
- `ub config show` 和 JSON Schema 能展示/校验新字段。

**Non-Goals:**

- 不自动从 provider `/models` 或远程 metadata 推断上下文窗口。
- 不为每个内置模型维护完整上下文窗口表。
- 不改变 token 估算算法本身。

## Decisions

1. 模型级字段放在 `config.ModelConfig.MaxContextTokens`。
   - 原因：该结构已经是用户覆盖模型能力的入口，配置层级与 reasoning 能力一致。
   - 备选：新增 provider-level `max_context_tokens`。该方案不能表达同一 provider 下不同模型窗口。

2. `modelinfo.Info` 携带 `MaxContextTokens`，CLI/TUI 在构造 Agent 时传给 `agent.Options`。
   - 原因：Agent 已经知道当前 model，但不应依赖完整 config；由 CLI/TUI 负责解析配置更符合现有 reasoning 解析路径。
   - 备选：把 provider config 传入 Agent。该方案扩大 Agent 依赖面。

3. Agent 内部使用 `effectiveMaxContextTokens()`。
   - 原因：集中处理模型级覆盖和 provider fallback，避免自动 summary 与 context status 分叉。

## Risks / Trade-offs

- 用户配置过大的窗口会推迟 summary -> 这是显式用户覆盖，状态栏会暴露 used/max 便于观察。
- 用户配置无效的非正数 -> 按未配置处理，回退 provider caps。
