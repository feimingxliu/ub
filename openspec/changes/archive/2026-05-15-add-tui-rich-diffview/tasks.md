## 1. 依赖与组件

- [x] 1.1 引入 Chroma 依赖并运行 `go mod tidy`。
- [x] 1.2 新增 `internal/tui/diffview` 包，定义 Model、构造函数、View 和文件切换方法。
- [x] 1.3 实现基于文件路径的 Chroma lexer 选择与 plaintext fallback。

## 2. Modal 集成

- [x] 2.1 permission modal 持有 diffview model，展开 preview 时渲染 diffview。
- [x] 2.2 modal 展开后支持左/右/上/下方向键切换 diff 文件。
- [x] 2.3 保持 `d` 展开/折叠和 `1`-`5` 决策按键行为不变。

## 3. 验证

- [x] 3.1 添加 diffview 单测，覆盖空 diff、单文件渲染、多文件循环切换。
- [x] 3.2 添加常见语言 Go/Python/TypeScript 高亮不 panic 单测。
- [x] 3.3 更新 permission modal 单测覆盖 diffview 展开和方向键切换。
- [x] 3.4 运行 `go test ./...`。
- [x] 3.5 运行 `make build`。
