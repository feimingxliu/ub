## 1. SDK 与 provider 注册

- [x] 1.1 添加 OpenAI 官方 Go SDK 依赖
- [x] 1.2 新增 `internal/provider/openai` 包并注册 `type: openai`
- [x] 1.3 更新 CLI provider 注册导入

## 2. OpenAI 调用与转换

- [x] 2.1 实现配置校验与 HTTP client/options，支持 api_key、base_url、headers、timeout
- [x] 2.2 实现内部文本消息到 OpenAI ChatCompletion messages 的转换
- [x] 2.3 实现 streaming delta 到 provider text_delta/usage/done 的转换
- [x] 2.4 实现非流式响应转换 helper
- [x] 2.5 对非文本 block 返回可读错误

## 3. 测试

- [x] 3.1 覆盖工厂创建、缺失 API key、unsupported block
- [x] 3.2 用 httptest SSE 覆盖 base_url、headers、请求体、流式 delta、done/EOF
- [x] 3.3 覆盖非流式响应转换
- [x] 3.4 覆盖 `ub chat --provider openai` CLI 路径

## 4. 验证

- [x] 4.1 运行 `go test ./...`
- [x] 4.2 运行 `make lint`
- [x] 4.3 运行 `make build`
- [x] 4.4 运行 `openspec validate add-openai-provider --strict`
