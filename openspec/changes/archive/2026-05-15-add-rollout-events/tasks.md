## 1. Rollout 核心

- [x] 1.1 新增 `internal/rollout` 事件类型、payload helper、Writer 和 Reader 接口
- [x] 1.2 基于 `store.Store` 实现 SQLite 事件追加
- [x] 1.3 实现按 session 顺序读取事件

## 2. CLI chat 集成

- [x] 2.1 为 `ub chat` 创建 session，标题取用户 prompt 摘要，模型写入 session
- [x] 2.2 写入 user_message、assistant_message、usage 事件
- [x] 2.3 provider 或 stream 错误时写入 error 事件并保留原错误返回
- [x] 2.4 确保 rollout 写入不污染 stdout

## 3. 测试

- [x] 3.1 覆盖事件字段校验、写入 100 条、读取顺序和跨 session 隔离
- [x] 3.2 覆盖写入后重新打开 store 仍可读取事件
- [x] 3.3 覆盖 `ub chat` 成功写入 session 与 rollout 事件
- [x] 3.4 覆盖 provider usage 和 error 事件写入

## 4. 验证

- [x] 4.1 运行 `go test ./...`
- [x] 4.2 运行 `make lint`
- [x] 4.3 运行 `make build`
- [x] 4.4 运行 `openspec validate add-rollout-events --strict`
