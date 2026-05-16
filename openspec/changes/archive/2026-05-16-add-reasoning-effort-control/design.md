## Context

当前 ub 已能通过 provider 体检获取模型列表，并能展示 provider 返回的 reasoning/thinking delta，但 `provider.Request` 没有 reasoning 配置，配置 schema 也没有模型能力描述。不同 provider 的 reasoning 参数形态不一致：OpenAI 使用 effort 字段，Anthropic 使用 thinking budget，OpenAI-compatible 网关能力更不稳定。

## Goals / Non-Goals

**Goals:**

- 在不额外计费 probe 的默认路径下，为当前模型解析可用 reasoning effort。
- 允许用户通过配置覆盖模型能力，兼容私有模型和兼容网关。
- 在 TUI 中通过 `/effort` 查看和切换当前模型支持的 effort。
- Provider adapter 只在模型能力声明支持时发送 reasoning 参数。

**Non-Goals:**

- 不实现自动按任务复杂度选择 effort。
- 不实现默认启动时的真实 API capability probe。
- 不迁移到 OpenAI Responses API。

## Decisions

1. **能力来源采用三层合并**

   模型能力按 `用户配置 > 内置能力表 > 保守启发式` 解析。模型列表仍由 provider 发现；reasoning 能力由内置表和配置补全。这样避免启动时产生额外 API 成本，同时允许用户修正网关模型。

2. **使用 provider-neutral ReasoningConfig**

   在 config 与 `provider.Request` 中引入统一的 `ReasoningConfig`，包含 `effort` 与 `summary`。Agent/CLI 只传递语义，OpenAI/Anthropic adapter 自行映射到后端字段。

3. **切换前严格校验**

   `/effort <value>` 必须校验当前模型的 `supported_efforts`。不支持时保持原值并显示错误；切换模型时，如果当前 effort 不再可用，回落到新模型默认 effort 或 `none`。

4. **Anthropic 使用预算映射**

   Anthropic 没有通用 effort 字段。ub 将 `low/medium/high/xhigh` 映射为固定 thinking budget，并确保 budget 小于 `max_tokens`。`none` 不发送 thinking。

5. **兼容 provider 保守发送**

   `openai-compat` 默认只有在模型能力声明支持 reasoning 时才发送 OpenAI-style `reasoning_effort`，避免兼容服务因未知字段报错。

## Risks / Trade-offs

- [Risk] 内置能力表可能滞后于新模型 → 用户配置可覆盖，doctor 后续可展示未知模型提示。
- [Risk] 兼容网关支持字段不统一 → 默认不对未知模型发送 reasoning；需要显式配置能力。
- [Risk] Anthropic budget 与 max_tokens 约束出错 → adapter 在构造请求时夹紧 budget，测试覆盖参数映射。
- [Risk] TUI 快捷键冲突 → 本次只新增 `/effort` slash 命令和候选，不占用 Shift+Tab。
