## 1. OpenAI 兼容 provider

- [x] 1.1 在 OpenAI provider 中抽出可复用兼容构造入口
- [x] 1.2 新增 `internal/provider/compat` 包并注册 `type: openai-compat`
- [x] 1.3 实现 compat 配置校验：强制 `base_url`，API key 可选
- [x] 1.4 覆盖 compat 工厂、流式 delta、usage/done、EOF 和 unsupported block 测试

## 2. Ollama provider

- [x] 2.1 新增 `internal/provider/ollama` 包并注册 `type: ollama`
- [x] 2.2 实现 Ollama 配置、默认 `base_url`、headers、timeout
- [x] 2.3 实现 text-only 消息到 `/api/chat` 请求体转换
- [x] 2.4 实现 NDJSON stream 到 text_delta/usage/done/EOF 的转换
- [x] 2.5 覆盖 Ollama 工厂、默认 base_url、请求体、流式事件和 unsupported block 测试

## 3. CLI 集成

- [x] 3.1 更新 CLI provider 注册导入
- [x] 3.2 覆盖 `ub chat --provider compat` CLI 路径
- [x] 3.3 覆盖 `ub chat --provider ollama` CLI 路径

## 4. 验证

- [x] 4.1 运行 `go test ./...`
- [x] 4.2 运行 `make lint`
- [x] 4.3 运行 `make build`
- [x] 4.4 运行 `openspec validate add-compat-and-ollama-providers --strict`
