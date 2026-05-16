## Why

ub 目前只能展示 provider 返回的 reasoning/thinking delta，不能主动为支持的模型设置思考等级。用户需要像模型切换一样，在 TUI 中明确查看并切换当前模型支持的 reasoning effort，同时避免把不兼容参数发给 provider。

## What Changes

- 增加模型能力解析：自动发现 provider 模型列表，并用内置能力表与用户配置补全 reasoning 支持情况。
- 增加 provider-neutral reasoning 配置，Agent/CLI 在调用 provider 时携带当前 effort。
- 增加 TUI `/effort` 命令，用于列出和切换当前模型支持的思考等级；非法 effort 不生效。
- 在状态栏展示当前 effort，并随模型切换自动回落到该模型的默认/可用 effort。
- OpenAI provider 将 effort 映射到 Chat Completions reasoning 参数；Anthropic provider 将 effort 映射到 thinking budget。
- 更新配置 schema、spec 和测试，保证不支持 reasoning 的模型不会收到 reasoning 参数。

## Capabilities

### New Capabilities

### Modified Capabilities

- `provider-runtime`: 增加模型能力解析、reasoning effort 类型、Request 字段和校验规则。
- `config-loader`: 增加 reasoning 与 provider model capability 配置，并更新 JSON Schema 覆盖。
- `tui-slash-commands`: 增加 `/effort` 命令、候选展示、状态栏展示和运行时切换语义。
- `openai-provider`: 增加 OpenAI reasoning effort 参数映射。
- `anthropic-provider`: 增加 Anthropic thinking budget 参数映射。

## Impact

- 影响配置结构、schema 生成、TUI slash 命令、Agent 到 provider 的请求结构，以及 OpenAI/Anthropic adapter。
- 不引入新外部依赖；默认不做计费 API probe。
- 对未配置或不支持 reasoning 的模型保持现有行为，不发送 reasoning 参数。
