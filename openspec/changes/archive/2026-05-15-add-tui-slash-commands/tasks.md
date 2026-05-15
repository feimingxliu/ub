## 1. Parser

- [x] 1.1 新增 slash command parser，支持 `model`、`mode`、`clear`、`sessions`、`help`、`quit`、`config`、`profile`。
- [x] 1.2 添加 parser 单测覆盖参数解析、空命令和未知命令。

## 2. TUI 执行

- [x] 2.1 TUI Enter 处理识别 `/` 输入，slash 命令不发送给 Agent。
- [x] 2.2 实现 `/clear`、`/help`、`/quit`、`/config`、`/sessions`、`/profile` 的本地行为。
- [x] 2.3 实现 `/model`、`/mode` 状态更新和 runner 同步。
- [x] 2.4 添加 model 单测覆盖 clear、help、quit、未知命令、model/mode 切换。

## 3. CLI Runner

- [x] 3.1 扩展 TUI runner 控制接口，支持更新 model 和 execution mode。
- [x] 3.2 CLI TUI runner 实现控制接口，后续 Agent turn 使用更新后的值。

## 4. 验证

- [x] 4.1 运行 `go test ./...`。
- [x] 4.2 运行 `make build`。
