## Why

Sprint 1 需要补齐 V1 规划中的四类 provider。`openai-compat` 是本地 vLLM、DeepSeek、Together、LM Studio 等服务的统一接入层，Ollama 则覆盖最常见的本地模型运行时。

## What Changes

- 新增 `internal/provider/compat`，复用 OpenAI Chat Completions 适配逻辑，注册 `type: openai-compat`。
- 新增 `internal/provider/ollama`，实现 Ollama `/api/chat` 的流式文本响应转换，注册 `type: ollama`。
- 更新 CLI provider 空导入，使 `ub chat --provider compat|ollama` 可通过统一工厂创建 provider。
- 补充 provider runtime 规格，覆盖 compat 与 Ollama 的工厂注册和 CLI 消费路径。

## Capabilities

### New Capabilities

- `compat-provider`: OpenAI 兼容 provider 的配置约束、消息转换、流式事件和错误行为。
- `ollama-provider`: Ollama provider 的配置、REST `/api/chat` 请求、流式事件和错误行为。

### Modified Capabilities

- `provider-runtime`: provider 工厂支持 `openai-compat` 与 `ollama` 类型，`ub chat` 可消费这两类 provider 的事件流。

## Impact

- 新增 `internal/provider/compat/` 与 `internal/provider/ollama/` 包及测试。
- 修改 CLI provider 注册导入。
- 不新增外部 SDK 依赖；Ollama 使用标准库 HTTP client。
