## 1. 包骨架

- [x] 1.1 新建 `internal/tool/` 目录与 `tool.go`、`registry.go` 文件
- [x] 1.2 在 `tool.go` 中加包级注释，说明此包仅负责接口与注册，非并发安全

## 2. 接口与数据类型

- [x] 2.1 在 `tool.go` 定义 `Risk` 类型与常量 `RiskSafe`、`RiskWrite`、`RiskExec`
- [x] 2.2 定义 `Tool` 接口：`Name`、`Description`、`Schema`、`Risk`、`Execute`
- [x] 2.3 定义 `Result` 与 `FileChange` 结构体并添加 JSON 标签
- [x] 2.4 定义 `Preview`、`FileDiff` 结构体（含 `Kind` 常量 `KindCreate`/`KindModify`/`KindDelete`）并添加 JSON 标签
- [x] 2.5 定义 `PreviewableTool` 可选接口

## 3. Registry

- [x] 3.1 在 `registry.go` 实现 `Registry` 与 `New()` 构造函数
- [x] 3.2 实现 `Register(t Tool) error`：重名返回错误且不覆盖
- [x] 3.3 实现 `Get(name string) (Tool, bool)`
- [x] 3.4 实现 `All() []Tool`：按名称升序排序返回
- [x] 3.5 实现 `Schemas() map[string]*jsonschema.Schema`

## 4. 单测

- [x] 4.1 `tool_test.go`：用 mock 类型覆盖 `Tool` 编译期断言、`Schema()` JSON 序列化
- [x] 4.2 `tool_test.go`：覆盖 `PreviewableTool` type assertion 在实现/未实现两种工具上的行为
- [x] 4.3 `tool_test.go`：覆盖 `Preview`、`Result` 的 JSON 字段名（含 `unified_diff` 等下划线命名）
- [x] 4.4 `registry_test.go`：覆盖注册成功、重名报错（断言原值保留）、未命中查询
- [x] 4.5 `registry_test.go`：覆盖 `All()` 排序、`Schemas()` 与 `All()` 一致

## 5. 验证

- [x] 5.1 运行 `go test ./internal/tool/...`
- [x] 5.2 运行 `make lint`
- [x] 5.3 运行 `make build`
- [x] 5.4 运行 `openspec validate add-tool-registry --strict`
