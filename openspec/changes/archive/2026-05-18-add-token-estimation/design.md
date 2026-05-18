## Context

当前 agent/provider 请求已经统一使用 `message.Message`，provider 也会在 stream 中返回可选 usage，但仓库还没有发请求前的上下文体量估算。Sprint 4 的自动 summary 需要先知道「当前消息大概占用多少 token」以及模型实际 usage 与估算之间的偏差。

## Goals / Non-Goals

**Goals:**

- 提供独立的 `internal/context` 包，作为 I-27 的最小上下文估算入口。
- 对 OpenAI 系模型优先使用 `tiktoken-go`，使常见模型的文本 token 数接近真实值。
- 对 Anthropic、Ollama、fake 或未知模型提供稳定字符近似，避免估算失败阻塞请求。
- 提供进程内 usage 校正，让调用方在拿到 provider usage 后可以修正后续同模型估算。

**Non-Goals:**

- 不实现 Anthropic 精确 BPE。
- 不在 I-27 触发自动 summary；I-28 单独接入 agent 请求路径。
- 不持久化校正缓存；进程重启后重新使用默认估算。

## Decisions

1. `internal/context` 目录使用包名 `context`，导出 `Estimate(msgs []message.Message, model string) int`，匹配 roadmap 的调用形状。需要同时导入标准库 `context` 的调用方可用别名导入该包。

2. 估算输入先序列化为 provider-neutral 文本帧：包含 role、text block、tool_use 名称与 input JSON、tool_result ID 与输出。这样工具消息和普通聊天消息都会计入估算，且估算与具体 provider SDK 解耦。

3. OpenAI 系模型使用 `tiktoken-go` 的 model encoding；encoding 不存在时回退到 `cl100k_base`，再失败时使用字符近似。非 OpenAI 系不尝试精确 BPE，直接使用字符近似。

4. usage 校正通过 `ObserveUsage(model string, estimated int, actual int)` 写入进程内倍率，`Estimate` 对同模型后续估算应用该倍率。倍率做合理范围限制，避免一次异常 usage 把估算拉到极端。

## Risks / Trade-offs

- [Risk] provider-neutral 文本帧无法完全匹配各 provider 的真实 chat 模板开销。→ 用固定 per-message 开销和 usage 校正降低偏差。
- [Risk] `tiktoken-go` 不认识新模型。→ 回退到 `cl100k_base` 或字符近似，保证估算总能返回非负整数。
- [Risk] 进程内校正对多模型混用不稳定。→ 校正按 canonical model key 分开保存，并限制倍率范围。
