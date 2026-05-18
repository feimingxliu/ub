## Why

Agent 在发起 provider 请求前需要知道历史消息大致会占用多少 token，否则后续自动摘要无法可靠判断是否接近模型上下文窗口。I-27 先提供独立、可测试的估算能力，为 I-28 的上下文压缩做前置基础。

## What Changes

- 新增 `internal/context` token 估算包，暴露 `Estimate(msgs []message.Message, model string) int`
- OpenAI 系模型优先使用 `tiktoken-go` 估算；无法匹配编码或非 OpenAI 系模型时回退到字符近似
- 支持按 provider 返回的 usage 对模型估算比例做进程内校正
- 覆盖已知字符串、消息结构和 usage 校正的单元测试

## Capabilities

### New Capabilities

- `context-management`: 覆盖 token 估算、上下文阈值判断和后续自动摘要的上下文管理行为

### Modified Capabilities

无。

## Impact

- 影响 `internal/context/` 新包及其单元测试
- 新增 `tiktoken-go` 依赖
- 后续 I-28 会复用该包进行自动 summary 触发判断
