## 1. SDK streaming 适配

- [x] 1.1 调研本地 Anthropic SDK streaming 事件类型和 accumulator API
- [x] 1.2 修改 Anthropic provider `Chat` 使用 `Messages.NewStreaming`
- [x] 1.3 实现 provider stream 包装，支持 text delta、usage、done、EOF

## 2. 取消与关闭

- [x] 2.1 `Next(ctx)` 在 context 取消时关闭 SDK stream 并返回 ctx 错误
- [x] 2.2 `Close()` 幂等且安全
- [x] 2.3 更新 Anthropic capabilities 标记 streaming 可用

## 3. 测试

- [x] 3.1 用 httptest SSE 覆盖多段 text delta 拼接
- [x] 3.2 覆盖 usage、done、done 后 EOF
- [x] 3.3 覆盖 context cancel 与重复 Close
- [x] 3.4 保留非文本 block 和 CLI chat 现有测试通过

## 4. 验证

- [x] 4.1 运行 `go test ./...`
- [x] 4.2 运行 `make lint`
- [x] 4.3 运行 `make build`
- [x] 4.4 运行 `openspec validate add-anthropic-streaming --strict`
