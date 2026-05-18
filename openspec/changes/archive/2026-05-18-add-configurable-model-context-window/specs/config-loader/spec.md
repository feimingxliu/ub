## ADDED Requirements

### Requirement: 模型级上下文窗口配置

配置系统 SHALL 支持在 `providers.<provider>.models.<model>.max_context_tokens` 中声明模型最大上下文 token 数。该字段 MUST 为可选正整数；未配置或小于等于 0 时 MUST 视为未覆盖。`ub config show` MUST 能输出该字段，JSON Schema MUST 覆盖该字段。

#### Scenario: 解析模型级 max_context_tokens

- **WHEN** 配置文件包含 `providers.vibecoding.models."openai/glm-5.1".max_context_tokens: 200000`
- **THEN** 加载后的 provider model config MUST 保留 `MaxContextTokens=200000`

#### Scenario: schema 覆盖模型级 max_context_tokens

- **WHEN** 用户运行 `make schema`
- **THEN** `schema/config.schema.json` MUST 包含 provider model config 的 `max_context_tokens` 字段
