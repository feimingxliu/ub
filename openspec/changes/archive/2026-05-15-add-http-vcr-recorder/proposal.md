## Why

I-06 需要在真实 provider 接入前提供 HTTP 录制 / 回放基础设施，让后续 Anthropic/OpenAI/Ollama 适配层可以用固定 cassette 做可重复测试。先实现 `http.RoundTripper` 级别的 vcr，可以把外部 LLM 调用从普通单元测试中隔离出来。

## What Changes

- 新增 `internal/vcr/` 包，实现可插拔的 `http.RoundTripper`。
- 支持 `record`、`replay`、`disabled` 三种模式，模式可由 `UB_VCR=record|replay|disabled` 或调用方参数决定。
- 定义 JSONL cassette 格式，每行保存一对 `{request, response}`。
- 录制时对请求 / 响应中的敏感 header 脱敏，例如 `Authorization`、`x-api-key`、`proxy-authorization`。
- 回放时按顺序匹配请求的 method、url、body hash；不匹配时返回清晰错误。
- 提供 `httptest.Server` 单测：record 写 cassette，再 replay 从 cassette 返回同样响应。
- 不实现自动 cassette 重命名、并发请求安全、流式增量语义解析或 provider 集成。

## Capabilities

### New Capabilities

- `http-vcr`: HTTP 请求录制 / 回放 transport、cassette JSONL 格式、敏感 header 脱敏和顺序请求匹配。

### Modified Capabilities

- 无。

## Impact

- 新增 `internal/vcr/` 生产代码与测试。
- 后续 provider 适配层可把该 transport 注入标准库 `http.Client`。
- 不新增外部依赖，不修改现有 CLI、config、store、message 行为。
