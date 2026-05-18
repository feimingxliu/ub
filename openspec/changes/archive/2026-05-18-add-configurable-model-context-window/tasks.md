## 1. 配置与模型能力

- [x] 1.1 在 `config.ModelConfig` 中增加 `max_context_tokens` 字段并补充配置加载测试。
- [x] 1.2 在 `modelinfo.Info` 中解析并暴露模型级 `MaxContextTokens`，补充单元测试。
- [x] 1.3 更新 JSON Schema。

## 2. Agent 接入

- [x] 2.1 在 `agent.Options` 中增加模型级最大上下文覆盖，并统一自动 summary/context status 的最大上下文取值。
- [x] 2.2 在 headless CLI 和 TUI runner 构造 Agent 时传入当前模型的 `MaxContextTokens`。
- [x] 2.3 补充 Agent 单元测试覆盖模型级窗口优先和 provider fallback。

## 3. 文档、配置与验证

- [x] 3.1 更新 requirements/design/roadmap 或 OpenSpec 主规格中的配置说明。
- [x] 3.2 将用户本地 `openai/glm-5.1` 配置更新为 `max_context_tokens: 200000`。
- [x] 3.3 运行相关测试和全量 `go test ./...`，完成 sync/archive/commit。
