## 1. 依赖与入口

- [x] 1.1 引入 Bubble Tea/Bubbles 依赖，并保持 `go mod tidy` 后模块干净。
- [x] 1.2 调整 cobra 根命令，使 `ub` 无子命令时启动 TUI，显式子命令行为不变。

## 2. TUI 骨架

- [x] 2.1 新增 `internal/tui` 包，定义 `Options`、根 model 与 `Run(ctx, Options) error`。
- [x] 2.2 实现消息列表、输入框、状态栏渲染，状态栏显示 model、execution mode 和 cwd。
- [x] 2.3 实现 Enter 发送非空输入并回显，空输入不新增消息。
- [x] 2.4 实现 Ctrl+C/Esc 退出。

## 3. 验证

- [x] 3.1 添加 TUI model 单测，覆盖输入回显、空输入忽略和退出命令。
- [x] 3.2 运行 `go test ./...`。
- [x] 3.3 运行 `go build ./...` 或 `make build` 验证二进制可构建。
