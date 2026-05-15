## 1. SDK 与 provider 注册

- [x] 1.1 添加 Anthropic 官方 Go SDK 依赖
- [x] 1.2 新增 `internal/provider/anthropic` 包并注册 `type: anthropic`
- [x] 1.3 更新 CLI provider 注册导入，使 `ub chat` 可创建 Anthropic provider

## 2. Anthropic 非流式调用

- [x] 2.1 实现配置校验与 HTTP client 构造，支持 `api_key`、`base_url`、`headers`、`timeout`
- [x] 2.2 实现内部文本消息到 Anthropic Messages 请求的转换
- [x] 2.3 实现 Anthropic 响应到 provider `text_delta`、`usage`、`done` 事件的转换
- [x] 2.4 对非文本 block 返回可读错误

## 3. 测试

- [x] 3.1 覆盖工厂创建、缺失 API key、未知/不支持 block 的单元测试
- [x] 3.2 用 httptest 或 VCR 覆盖 base_url、headers、请求 body 与响应事件转换
- [x] 3.3 覆盖 `ub chat --provider anthropic` 的 CLI 路径

## 4. 验证

- [x] 4.1 运行 `go test ./...`
- [x] 4.2 运行 `make lint`
- [x] 4.3 运行 `make build`
- [x] 4.4 运行 `openspec validate add-anthropic-provider --strict`
