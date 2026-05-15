# provider-runtime Specification

## Purpose

Define the SDK-neutral provider runtime, deterministic fake provider, provider factory, and minimal `ub chat` CLI behavior.

## Requirements

### Requirement: Provider 接口与事件流

系统 SHALL 在 `internal/provider` 中提供 SDK 无关的 provider 抽象。Provider MUST 暴露名称、能力描述和 `Chat(ctx, Request) (Stream, error)`；Stream MUST 通过 `Next(ctx)` 顺序返回事件，并支持 `Close()`。

#### Scenario: Provider 返回顺序事件

- **WHEN** 调用 provider 的 `Chat` 并持续调用 `Stream.Next`
- **THEN** 系统按 provider 生成顺序返回事件，直到 `done` 事件或流结束

#### Scenario: Context 取消

- **WHEN** 调用方在读取 stream 时取消 context
- **THEN** `Next` MUST 返回 context 取消错误，且 `Close` MUST 可安全调用

### Requirement: Provider 工厂

系统 SHALL 根据配置中的 provider `type` 创建 provider 实例。工厂 MUST 支持 `fake` 类型；未知类型 MUST 返回可读错误。

#### Scenario: 创建 fake provider

- **GIVEN** 配置项 `providers.test.type=fake`
- **WHEN** 调用 provider 工厂创建 `test`
- **THEN** 返回名称为 `test` 且能力可查询的 fake provider

#### Scenario: 未知 provider 类型

- **GIVEN** 配置项 `providers.bad.type=unknown`
- **WHEN** 调用 provider 工厂创建 `bad`
- **THEN** 返回包含未知类型和 provider 名称的 error

### Requirement: Fake provider 脚本

系统 SHALL 提供 `internal/provider/fake`，可按预设脚本顺序产生 `text_delta`、`tool_call`、`usage`、`done` 和 `error` 事件。fake provider MUST 支持通过 Go 代码直接构造，也 MUST 支持从配置脚本构造。

#### Scenario: Go 代码构造脚本

- **WHEN** 测试代码用 `fake.New(fake.Script{fake.TextDelta("hi"), fake.Done()})` 构造 provider
- **THEN** 读取 stream 时依次得到文本 delta 和 done 事件

#### Scenario: 配置脚本构造

- **GIVEN** YAML 配置中 fake provider 含 `script` 列表
- **WHEN** provider 工厂创建该 provider
- **THEN** fake provider MUST 按配置列表顺序产生事件

#### Scenario: tool_call 保留输入

- **WHEN** fake 脚本包含 tool_call 事件，输入为 JSON 对象
- **THEN** provider 事件 MUST 保留工具名和原始 JSON input

### Requirement: 最小 chat 命令

系统 SHALL 提供 `ub chat` 子命令用于 provider 对话。命令 MUST 支持 `ub chat "prompt"`、`ub chat -`、`--provider <name>`、`--model <id>`、`--session <id>` 和 `--new`；文本 delta MUST 流式写到 stdout。`--provider` 与 `--model` MUST 只影响当前调用，不写回配置。未传 `--provider` 时，命令 MUST 使用 `default_provider`；若未配置 `default_provider`，MUST 使用配置中第一个可用 provider。命令 MUST NOT 从 `default_model` 的 `/` 前缀推断 provider。

#### Scenario: 参数 prompt

- **GIVEN** 配置中存在 fake provider，脚本输出文本 `pong`
- **WHEN** 用户运行 `ub chat --provider fake "ping"`
- **THEN** stdout 包含 `pong`，命令返回成功

#### Scenario: stdin prompt

- **GIVEN** 配置中存在 fake provider
- **WHEN** 用户运行 `ub chat --provider fake -` 并从 stdin 提供 prompt
- **THEN** 命令使用 stdin 内容作为用户消息并输出 provider 文本

#### Scenario: provider 覆盖

- **GIVEN** 配置中有多个 provider
- **WHEN** 用户运行 `ub chat --provider test "hi"`
- **THEN** 命令 MUST 使用名为 `test` 的 provider

#### Scenario: 默认 provider 配置

- **GIVEN** 配置中设置 `default_provider: vibecoding`，且 `default_model: openai/glm-5.1`
- **WHEN** 用户运行 `ub chat "hi"`
- **THEN** 命令 MUST 使用 `vibecoding` provider，并把完整 model `openai/glm-5.1` 传给 provider

#### Scenario: 不从 default_model 推断 provider

- **GIVEN** 配置中只有 `providers.vibecoding`，且 `default_model: openai/glm-5.1`
- **WHEN** 用户运行 `ub chat "hi"`
- **THEN** 命令 MUST 使用 `vibecoding` provider，而不是尝试使用名为 `openai` 的 provider

#### Scenario: tool_call 暂不执行

- **GIVEN** fake provider 返回 tool_call 事件
- **WHEN** 用户运行 `ub chat`
- **THEN** 命令 MUST 返回可读错误，说明裸 chat 暂不执行工具调用

#### Scenario: 继续 session

- **GIVEN** 已有 session 中存在 user 与 assistant 历史消息
- **WHEN** 用户运行 `ub chat --session <id> "next"`
- **THEN** provider request MUST 包含历史消息和当前 user 消息，并把新事件追加到同一 session

#### Scenario: 强制新 session

- **WHEN** 用户运行 `ub chat --new "hello"`
- **THEN** 命令 MUST 创建新 session，而不是复用任何已有 session

#### Scenario: session 参数冲突

- **WHEN** 用户同时传入 `--session <id>` 和 `--new`
- **THEN** 命令 MUST 返回可读错误

#### Scenario: provider 不存在

- **WHEN** 用户运行 `ub chat --provider missing "hi"`
- **THEN** 命令 MUST 返回说明 provider 未配置的可读错误

### Requirement: Anthropic provider 工厂注册

provider 工厂 SHALL 支持 `type: anthropic`，使 CLI 和测试能通过统一 `provider.New` 创建 Anthropic provider。

#### Scenario: `ub chat` 使用 anthropic provider

- **GIVEN** 配置中存在名为 `anthropic` 且类型为 `anthropic` 的 provider
- **WHEN** 用户运行 `ub chat --provider anthropic --model claude-test "ping"`
- **THEN** CLI MUST 通过 provider 工厂创建 Anthropic provider 并消费其事件流

### Requirement: OpenAI provider 工厂注册

provider 工厂 SHALL 支持 `type: openai`，使 CLI 和测试能通过统一 `provider.New` 创建 OpenAI provider。

#### Scenario: `ub chat` 使用 openai provider

- **GIVEN** 配置中存在名为 `openai` 且类型为 `openai` 的 provider
- **WHEN** 用户运行 `ub chat --provider openai --model gpt-test "ping"`
- **THEN** CLI MUST 通过 provider 工厂创建 OpenAI provider 并消费其事件流

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

### Requirement: Chat rollout 持久化

`ub chat` SHALL 把单轮对话绑定到 SQLite session，并写入 rollout 事件。该持久化 MUST 不改变 stdout 的文本输出行为。

#### Scenario: chat 创建 session

- **GIVEN** 默认 store 为空
- **WHEN** 用户运行一次成功的 `ub chat`
- **THEN** `ub sessions ls` MUST 能列出新 session

#### Scenario: chat stdout 不受 rollout 影响

- **GIVEN** provider 输出文本 `pong`
- **WHEN** 用户运行 `ub chat`
- **THEN** stdout MUST 仍只包含 provider 文本，不包含 rollout metadata

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
