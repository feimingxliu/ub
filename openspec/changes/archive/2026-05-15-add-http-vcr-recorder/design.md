## Context

后续 provider 适配层会依赖真实 HTTP API。为了让 CI 和本地单测不需要真实 API key，I-06 需要先提供一个标准库 `http.RoundTripper` 级别的录制 / 回放组件。当前仓库已有 message、config、store、log 基建，但还没有可复用的 HTTP cassette 机制。

## Goals / Non-Goals

**Goals:**

- 新增 `internal/vcr`，提供 `Recorder` 实现 `http.RoundTripper`。
- 支持 `record`、`replay`、`disabled` 三种模式，并能从 `UB_VCR` 解析模式。
- cassette 使用 JSONL，每行一条 `{request, response}` 记录。
- 录制请求与响应时自动脱敏敏感 header。
- replay 按 cassette 顺序匹配 `method + url + body_sha256`。
- 单测覆盖 record → replay 的完整路径。

**Non-Goals:**

- 不实现并发安全录制 / 回放；V1 假定单测试顺序请求。
- 不自动生成或重命名 cassette 文件。
- 不解析 provider stream 语义，不拆分 SSE chunk。
- 不接入 provider 包或 CLI。
- 不引入外部依赖。

## Decisions

### 1. API 形态

核心 API：

```go
type Mode string
const (
    ModeDisabled Mode = "disabled"
    ModeRecord   Mode = "record"
    ModeReplay   Mode = "replay"
)

type Recorder struct { ... }
func New(cassettePath, mode string, base http.RoundTripper) (*Recorder, error)
func ModeFromEnv() Mode
func (r *Recorder) RoundTrip(req *http.Request) (*http.Response, error)
```

`base` 为空时使用 `http.DefaultTransport`。`disabled` 模式直接调用 base，不读写 cassette；这样 provider 后续可以无条件包一层 vcr。

### 2. Cassette JSONL 结构

每行一条记录：

```json
{
  "request": {
    "method": "POST",
    "url": "https://api.example.test/v1/messages",
    "headers": {"Content-Type": ["application/json"]},
    "body_sha256": "..."
  },
  "response": {
    "status_code": 200,
    "headers": {"Content-Type": ["application/json"]},
    "body": "base64..."
  }
}
```

响应 body 用 base64，避免二进制或换行破坏 JSONL。请求 body 不保存原文，只保存 hash，减少 secret 泄露面；后续如需要调试请求体可另开 debug 选项。

### 3. Record 模式

Record 模式读取并恢复请求 body，调用 base transport，然后完整读取响应 body。写 cassette 时保存脱敏后的 request metadata、response status/header/body。返回给调用方的 response 必须重新填充 body，不能因为录制而被消费。

写文件使用追加模式，父目录自动创建。每条记录编码为单行 JSON 并立即写入；本迭代不做 fsync。

### 4. Replay 模式

Replay 模式初始化时读取 cassette 全部 JSONL 到内存，维护当前位置 `next`。每个请求读取 body、计算 hash，并与 `records[next].request` 比较。匹配失败返回包含 expected/actual 的错误；匹配成功时构造 `http.Response`，body 从 base64 解码得到。

顺序敏感是刻意选择：它能捕获 provider 适配层请求顺序变化，且避免复杂的多请求匹配索引。

### 5. Header 脱敏

敏感 header 名大小写不敏感，至少包括 `authorization`、`proxy-authorization`、`x-api-key`、`api-key`、`x-auth-token`、`x-api-token`、`cookie`、`set-cookie`。命中后值统一替换为 `***`。非敏感 header 保留原值。

## Risks / Trade-offs

- [Risk] 请求 body 只存 hash，不便于肉眼调试 cassette → 优先降低 secret 泄露风险；错误信息展示 method/url/hash 足以定位大多数匹配问题。
- [Risk] 顺序 replay 不支持并发请求 → I-06 明确不支持并发；后续需要并发时再引入请求索引或锁。
- [Risk] 读取完整响应 body 会影响 streaming provider 测试 → 本迭代目标是 HTTP 录制基础设施；流式语义在 provider 集成测试中按完整 body replay。
