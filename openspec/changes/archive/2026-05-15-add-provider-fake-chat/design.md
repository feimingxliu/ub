## Context

当前仓库已有配置加载、消息模型、SQLite session store 和 HTTP VCR，但还没有 provider 抽象，也没有能真正走一次对话的 CLI 入口。后续真实 provider、rollout 写入、agent loop 和 TUI 都依赖同一套 provider 事件流。

## Goals / Non-Goals

**Goals:**

- 定义 provider-neutral 的 `Provider`、`Request`、`Stream`、`Event` 和 `Caps`。
- 实现 `fake` provider，用脚本顺序产生 text delta、tool call、usage、done、error 等事件。
- 实现 provider 工厂，按 `config.ProviderConfig.Type` 创建 provider。
- 实现最小 `ub chat`，可用 fake provider 离线输出文本。

**Non-Goals:**

- 不实现 Anthropic、OpenAI、Ollama 等真实 provider。
- 不写 rollout，不创建或恢复 session。
- 不执行工具调用；I-07 只把 tool_call 作为事件类型暴露给后续 agent loop。

## Decisions

- **事件流接口以 pull 模式实现。** `Stream.Next(ctx)` 返回下一个事件，直到 `EventDone` 或 `io.EOF`。这让 CLI、agent loop 和测试都能用同一控制流处理取消与关闭。
- **fake provider 同时支持配置与代码构造。** 配置脚本用于 CLI 冒烟，`fake.New(fake.Script{...})` 用于单元测试，避免测试依赖 YAML。
- **`ub chat` 只消费文本事件。** `text_delta` 直接写 stdout；`usage` 忽略；`done` 结束；`tool_call` 在 I-07 返回清晰错误，避免假装工具已经接入。
- **模型选择使用显式覆盖优先。** `--model` 覆盖 config `default_model`；`--provider` 覆盖从模型前缀推导出的 provider；未指定 provider 时从 `default_model` 的 `<provider>/<model>` 解析。
- **fake 脚本字段保留在 `ProviderConfig`。** 这是开发期 provider 的配置输入，后续真实 provider 会忽略该字段。

## Risks / Trade-offs

- **fake 事件类型过早扩张** -> 保持字段小而稳定，只定义后续 provider 共同需要的文本、工具调用、usage、done、error。
- **CLI 与未来 rollout/session 逻辑耦合** -> I-07 的 `chat` 不写 session，后续 I-09/I-14 再把持久化与 history 接入。
- **配置脚本表达能力有限** -> 先支持顺序脚本，多 turn 条件脚本保留为 fake 包的内部扩展点。

## Migration Plan

新增代码不改变现有命令行为；`ub run` 仍保持占位。若回滚，删除 `internal/provider`、`chat` 子命令和 fake 脚本配置字段即可。
