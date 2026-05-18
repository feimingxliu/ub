## 1. 估算包

- [x] 1.1 新增 `internal/context` 包和 `Estimate(msgs []message.Message, model string) int` 入口
- [x] 1.2 实现 provider-neutral 消息文本帧，覆盖 role、text、tool_use 和 tool_result
- [x] 1.3 接入 `tiktoken-go`，OpenAI 系模型使用 tiktoken encoding，未知或非 OpenAI 模型使用字符近似回退
- [x] 1.4 实现 `ObserveUsage(model string, estimated int, actual int)` 进程内校正，并按模型隔离校正倍率

## 2. 验证与收尾

- [x] 2.1 添加 OpenAI 已知字符串、空消息、工具消息和未知模型回退的单元测试
- [x] 2.2 添加 usage 校正单元测试
- [x] 2.3 运行 `go test ./...`
