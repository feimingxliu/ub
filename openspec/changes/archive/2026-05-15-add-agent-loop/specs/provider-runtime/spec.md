## ADDED Requirements

### Requirement: Provider 工具请求

provider Request SHALL 支持携带工具定义。每个工具定义 MUST 包含名称、描述和 input schema。Provider MUST 在支持工具时把这些定义转换到后端 API；不支持工具的 provider 在收到非空工具定义时 MUST 返回可读错误。

#### Scenario: Request 包含工具定义

- **GIVEN** Registry 中注册了 `read` 工具
- **WHEN** Agent 调用 provider Chat
- **THEN** provider Request MUST 包含 `read` 的名称、描述和 JSON schema

### Requirement: fake provider 多轮脚本

fake provider SHALL 支持按 Chat 调用次数返回不同脚本，便于测试 "tool_call → tool_result → final answer" 的 agent loop。若未配置多轮脚本，fake provider MUST 保持现有单脚本行为。

#### Scenario: 第二轮读取 tool_result 后回答

- **GIVEN** fake provider 第一轮返回 tool_call，第二轮返回文本 `done`
- **WHEN** Agent 把 tool_result 追加到第二轮 provider request
- **THEN** fake provider MUST 返回第二轮脚本文本
