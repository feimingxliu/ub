## 1. 包与模式

- [x] 1.1 创建 `internal/vcr/` 目录
- [x] 1.2 定义 `Mode`、`ModeDisabled`、`ModeRecord`、`ModeReplay`
- [x] 1.3 实现 `ParseMode(raw string) (Mode, error)`
- [x] 1.4 实现 `ModeFromEnv() Mode`，未设置 `UB_VCR` 时返回 `disabled`
- [x] 1.5 定义 `Recorder` 类型和 `New(cassettePath, mode string, base http.RoundTripper) (*Recorder, error)`

## 2. Cassette 数据结构

- [x] 2.1 定义 JSONL record 结构：request 与 response
- [x] 2.2 request 结构包含 method、url、headers、body_sha256
- [x] 2.3 response 结构包含 status_code、headers、body(base64)
- [x] 2.4 实现 cassette 读取：逐行 decode JSON，空行跳过，错误带行号
- [x] 2.5 实现 cassette 写入：父目录自动创建，追加单行 JSON

## 3. Request / Response 捕获

- [x] 3.1 实现读取请求 body 后恢复 `req.Body`
- [x] 3.2 实现 `body_sha256` 计算
- [x] 3.3 实现读取响应 body 后恢复 `resp.Body`
- [x] 3.4 实现 response body base64 编码 / 解码
- [x] 3.5 实现 header clone，避免修改原始 request/response

## 4. Header 脱敏

- [x] 4.1 实现大小写不敏感的敏感 header 判断
- [x] 4.2 覆盖 `Authorization`、`Proxy-Authorization`、`x-api-key`、`api-key`、`x-auth-token`、`x-api-token`、`Cookie`、`Set-Cookie`
- [x] 4.3 写 cassette 前将敏感 header 值替换为 `***`
- [x] 4.4 保留非敏感 header 原值

## 5. RoundTripper 行为

- [x] 5.1 `disabled` 模式直接调用 base transport，不读写 cassette
- [x] 5.2 `record` 模式调用 base transport，并把交互追加写入 cassette
- [x] 5.3 `record` 模式返回给调用方的 response body 仍可读取
- [x] 5.4 `replay` 模式不调用 base transport，使用 cassette 构造 response
- [x] 5.5 `replay` 模式按顺序匹配 method、url、body_sha256
- [x] 5.6 replay 不匹配时返回包含 expected / actual 的错误
- [x] 5.7 replay cassette 耗尽时返回明确错误

## 6. 单元测试

- [x] 6.1 测试 `ParseMode` 与 `ModeFromEnv`
- [x] 6.2 测试 disabled 模式透传且不创建 cassette
- [x] 6.3 测试 record 对 `httptest.Server` 发真实请求并写 JSONL cassette
- [x] 6.4 测试 record 后调用方仍可读取 response body
- [x] 6.5 测试 cassette response body 是 base64
- [x] 6.6 测试 replay 返回 cassette 响应且不调用 base transport
- [x] 6.7 测试 replay method/url/body hash 不匹配错误
- [x] 6.8 测试 replay cassette exhausted 错误
- [x] 6.9 测试敏感 header 脱敏和非敏感 header 保留
- [x] 6.10 测试 record → replay 往返，server 关闭后 replay 仍成功

## 7. 验证与收尾

- [x] 7.1 运行 `gofmt` / `gofumpt` 覆盖新增 Go 文件
- [x] 7.2 运行 `go test ./...`
- [x] 7.3 运行 `make lint`
- [x] 7.4 运行 `make build`
- [x] 7.5 确认本次没有新增外部依赖
