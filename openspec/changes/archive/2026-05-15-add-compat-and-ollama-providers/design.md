## Context

当前 provider runtime 已有 fake、Anthropic 和 OpenAI。I-12 需要补齐 V1 provider 列表中的 `openai-compat` 与 `ollama`，并为 I-13 的 dev profile 和 doctor 提供本地模型路径。

## Goals / Non-Goals

**Goals:**

- `openai-compat` 复用 OpenAI Chat Completions 请求/流式转换逻辑。
- `openai-compat` 强制要求 `base_url`，允许本地服务不配置 `api_key`。
- `ollama` 使用 Ollama `/api/chat` 流式接口，支持 text-only system/user/assistant 消息。
- 两个 provider 均支持 `headers`、`timeout`，并可通过 `ub chat` 消费事件流。

**Non-Goals:**

- 不实现 tool calls、vision、reasoning content 或模型能力自动发现。
- 不要求本机实际运行 Ollama；测试使用 `httptest` 覆盖协议行为。
- 不处理 Azure OpenAI 的特殊 deployment path；如需支持，后续通过 compat 的 `base_url` 约定扩展。

## Decisions

- **compat 复用 OpenAI provider。** 在 `internal/provider/openai` 中提供兼容构造入口，`compat` 包只负责类型注册和配置校验，避免复制 ChatCompletion 转换逻辑。
- **compat API key 可选。** 本地 vLLM、LM Studio、Ollama `/v1` 常不需要鉴权；若缺省则传入占位 key 以满足 OpenAI SDK 的 Authorization header 形状。
- **Ollama 直接实现 REST `/api/chat`。** 该接口是 Ollama 原生协议，可读取 `prompt_eval_count`、`eval_count` 等 usage 元数据，比绕 `/v1` 更贴近本地运行时。
- **Ollama 默认 base_url。** 未配置时使用 `http://localhost:11434`；测试和 dev profile 可覆盖。

## Risks / Trade-offs

- **OpenAI 兼容服务协议差异** -> I-12 只覆盖 Chat Completions 文本流；特殊字段或 provider-specific 参数留到后续。
- **Ollama NDJSON 中途断流** -> stream 返回底层 scanner/JSON 错误；成功 done 后再返回 EOF。
- **本地服务无 API key** -> compat 不强制 key，但仍允许用户通过 `api_key` 或 `headers` 配置网关鉴权。
