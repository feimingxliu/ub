## ADDED Requirements

### Requirement: OpenAI 兼容 provider 工厂注册

provider 工厂 SHALL 支持 `type: openai-compat`，使 CLI 和测试能通过统一 `provider.New` 创建 OpenAI 兼容 provider。

#### Scenario: `ub chat` 使用 openai-compat provider

- **GIVEN** 配置中存在名为 `compat` 且类型为 `openai-compat` 的 provider
- **WHEN** 用户运行 `ub chat --provider compat --model local-test "ping"`
- **THEN** CLI MUST 通过 provider 工厂创建 OpenAI 兼容 provider 并消费其事件流

### Requirement: Ollama provider 工厂注册

provider 工厂 SHALL 支持 `type: ollama`，使 CLI 和测试能通过统一 `provider.New` 创建 Ollama provider。

#### Scenario: `ub chat` 使用 ollama provider

- **GIVEN** 配置中存在名为 `ollama` 且类型为 `ollama` 的 provider
- **WHEN** 用户运行 `ub chat --provider ollama --model qwen2.5-coder:1.5b "ping"`
- **THEN** CLI MUST 通过 provider 工厂创建 Ollama provider 并消费其事件流
