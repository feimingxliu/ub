## 1. Chat flags 与参数

- [x] 1.1 新增 `ub chat --session <id>` 和 `--new` flags
- [x] 1.2 校验 `--session` 与 `--new` 冲突
- [x] 1.3 覆盖 provider/model 临时覆盖与 provider 缺失错误测试

## 2. Session 继续与 history

- [x] 2.1 重构 chat session 启动逻辑，支持新建或读取已有 session
- [x] 2.2 从 rollout user/assistant 事件重建 provider history
- [x] 2.3 计算下一 turn 并把新事件追加到同一 session
- [x] 2.4 成功结束后更新 session title/model/updated_at

## 3. 测试

- [x] 3.1 覆盖 `--session` 继续时 provider 收到历史消息
- [x] 3.2 覆盖 `--new` 创建新 session
- [x] 3.3 覆盖缺失 session 与 flag 冲突错误
- [x] 3.4 覆盖错误事件仍写入目标 session

## 4. 验证

- [x] 4.1 运行 `go test ./...`
- [x] 4.2 运行 `make lint`
- [x] 4.3 运行 `make build`
- [x] 4.4 运行 `openspec validate complete-chat-cli --strict`
