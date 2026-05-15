## 1. Agent 事件

- [x] 1.1 在 `internal/agent` 定义运行事件类型与可选 `EventSink`。
- [x] 1.2 在 Agent stream 消费、工具执行和完成/错误路径发出事件，保持未配置回调时行为不变。
- [x] 1.3 添加 Agent 事件回调单测。

## 2. TUI Agent 桥接

- [x] 2.1 在 `internal/tui` 定义 Runner 接口、流式事件类型和 channel 消费命令。
- [x] 2.2 扩展消息列表，支持 assistant delta 追加和工具状态消息。
- [x] 2.3 扩展状态栏显示 turn 和 running/idle 状态。
- [x] 2.4 发送用户输入后启动 Runner，运行中禁止重复发送。

## 3. CLI 组装

- [x] 3.1 在 root TUI 启动路径创建真实 provider、tool registry、permission manager 和 rollout session runner。
- [x] 3.2 TUI runner 维护 history/turn，并把 Agent 事件转发到 TUI channel。
- [x] 3.3 I-24 前对需要人工审批的 exec 请求先拒绝，避免无 modal 时静默执行。

## 4. 验证

- [x] 4.1 添加 TUI fake runner 单测，覆盖 delta 追加、Done 状态和运行中禁止重复发送。
- [x] 4.2 运行 `go test ./...`。
- [x] 4.3 运行 `make build`。
