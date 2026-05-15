## 1. Provider 核心

- [x] 1.1 新增 `internal/provider` 核心类型：`Provider`、`Caps`、`Request`、`Stream`、`Event`、`Usage`
- [x] 1.2 实现 provider 工厂，支持按配置创建 `fake` provider，并对未知类型返回可读错误
- [x] 1.3 为 provider 核心类型和工厂补充单元测试

## 2. Fake provider

- [x] 2.1 新增 `internal/provider/fake`，支持 Go 代码脚本构造和顺序事件流
- [x] 2.2 支持从 `config.ProviderConfig.Script` 构造 fake 脚本事件
- [x] 2.3 覆盖文本、tool_call、usage、done、error、ctx cancel 和 close 的单元测试

## 3. 配置与 CLI

- [x] 3.1 扩展 `config.ProviderConfig`，解析并保留 fake provider `script` 字段
- [x] 3.2 新增 `ub chat` 子命令，支持参数 prompt、stdin、`--provider`、`--model`
- [x] 3.3 让 `ub chat` 从配置选择 provider 和模型，流式输出 text delta，并对 tool_call 返回明确错误
- [x] 3.4 为 `ub chat` 增加 CLI 单元测试和 fake provider 冒烟覆盖

## 4. 验证

- [x] 4.1 运行 `go test ./...`
- [x] 4.2 运行 `make lint`
- [x] 4.3 运行 `make build`
- [x] 4.4 运行 `openspec validate add-provider-fake-chat --strict`
