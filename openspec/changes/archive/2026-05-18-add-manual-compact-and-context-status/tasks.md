## 1. Agent compact 与上下文用量

- [x] 1.1 抽出自动 summary 与手动 compact 共用的历史压缩逻辑。
- [x] 1.2 增加 Agent 手动 compact 入口，成功时更新 history 并写入 rollout Summary，历史不足时返回可读无操作结果。
- [x] 1.3 在 Agent runtime event 中携带 context used/max/ratio，并在请求准备和 compact 后发送更新。
- [x] 1.4 补充 Agent 单元测试覆盖手动 compact、无可压缩历史和 context usage event。

## 2. TUI 命令与状态栏

- [x] 2.1 增加 `/compact` slash command 解析、帮助和候选提示。
- [x] 2.2 增加 TUI compact runner 接口与命令执行路径，确保 `/compact` 不发送给普通 prompt runner。
- [x] 2.3 状态栏展示最近一次 context token 使用量，覆盖 max 未知与宽度收缩场景。
- [x] 2.4 补充 TUI 单元测试覆盖 `/compact` 成功、不可用提示、context status 更新和 slash parser。

## 3. CLI runner、文档与验证

- [x] 3.1 在 CLI TUI runner 中实现 compact，复用 Agent compact 入口并转换 context usage event。
- [x] 3.2 更新 requirements、design、roadmap 中的 `/compact` 和 context status 说明。
- [x] 3.3 运行相关包测试与全量 `go test ./...`。
