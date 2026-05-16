## 1. 配置与模型能力

- [x] 1.1 在配置类型中加入 reasoning、approval_agent.reasoning 和 provider models 能力覆盖字段，并更新 merge/redact/schema 相关逻辑。
- [x] 1.2 新增模型能力解析模块：合并内置表、provider 发现模型列表、用户配置覆盖，提供 effort 校验与默认值选择。
- [x] 1.3 为配置解析、模型能力覆盖、默认/未知模型行为补充单元测试。

## 2. Provider 请求与映射

- [x] 2.1 在 provider Request 中加入可选 reasoning 配置，并让 Agent、run、chat、TUI runner 按当前模型能力传入合法 effort。
- [x] 2.2 实现 OpenAI/OpenAI-compatible reasoning effort 请求参数映射，并覆盖 none/非 none 测试。
- [x] 2.3 实现 Anthropic thinking budget 映射，并覆盖 none/非 none 测试。

## 3. TUI 交互

- [x] 3.1 增加 `/effort` slash 命令、帮助文案、候选匹配和无参数候选展示。
- [x] 3.2 扩展 TUI runner 控制接口，支持列出/设置 effort，并在非法 effort 时保持原值。
- [x] 3.3 在状态栏展示当前 effort，并在模型切换后回落到新模型可用的默认 effort。
- [x] 3.4 补充 TUI 单元测试，覆盖 `/effort` 列表、切换、非法值和候选提示。

## 4. 文档与验证

- [x] 4.1 更新 requirements/design 中的 reasoning effort、配置和 doctor/model capability 描述。
- [x] 4.2 运行 gofmt、make schema、go test ./... 和 openspec 验证，确认 change 可归档。
