## Why

Sprint 1 后续 provider、rollout、agent loop 都需要一个统一的 provider 抽象和可离线验证的对话入口。先实现 fake provider 与最小 `ub chat`，可以在无 API key 的情况下端到端验证消息流。

## What Changes

- 新增 `internal/provider` 核心接口、能力描述、请求、流和事件类型。
- 新增 `internal/provider/fake`，支持配置脚本和 Go 代码脚本两种构造方式。
- 新增 provider 工厂，按配置中的 `type` 路由到 fake provider。
- 新增最小 `ub chat` 子命令，支持 prompt 参数、stdin、`--provider`、`--model`，并把 text delta 流式输出到 stdout。

## Capabilities

### New Capabilities

- `provider-runtime`: provider 接口、fake provider、provider 工厂和最小 CLI chat 运行时行为。

### Modified Capabilities

- `config-loader`: provider 配置需要支持 fake provider 的脚本字段。

## Impact

- 新增 `internal/provider/` 与 `internal/provider/fake/` 包。
- 修改 `internal/config.ProviderConfig`，加入 fake provider 脚本配置字段。
- 修改 `internal/cli`，加入 `chat` 子命令和相关测试。
- 不引入真实网络 provider，不写 rollout，不接入工具调用执行。
