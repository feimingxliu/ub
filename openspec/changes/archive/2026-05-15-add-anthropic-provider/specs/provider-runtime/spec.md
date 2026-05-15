## ADDED Requirements

### Requirement: Anthropic provider 工厂注册

provider 工厂 SHALL 支持 `type: anthropic`，使 CLI 和测试能通过统一 `provider.New` 创建 Anthropic provider。

#### Scenario: `ub chat` 使用 anthropic provider

- **GIVEN** 配置中存在名为 `anthropic` 且类型为 `anthropic` 的 provider
- **WHEN** 用户运行 `ub chat --provider anthropic --model claude-test "ping"`
- **THEN** CLI MUST 通过 provider 工厂创建 Anthropic provider 并消费其事件流
