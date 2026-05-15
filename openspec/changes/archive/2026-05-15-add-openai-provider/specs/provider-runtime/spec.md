## ADDED Requirements

### Requirement: OpenAI provider 工厂注册

provider 工厂 SHALL 支持 `type: openai`，使 CLI 和测试能通过统一 `provider.New` 创建 OpenAI provider。

#### Scenario: `ub chat` 使用 openai provider

- **GIVEN** 配置中存在名为 `openai` 且类型为 `openai` 的 provider
- **WHEN** 用户运行 `ub chat --provider openai --model gpt-test "ping"`
- **THEN** CLI MUST 通过 provider 工厂创建 OpenAI provider 并消费其事件流
