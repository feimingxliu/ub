## 1. Provider runtime 与 rollout

- [x] 1.1 在 `provider.Request` 中加入工具定义结构，包含 name/description/schema
- [x] 1.2 更新 fake provider，支持多轮脚本并保留单脚本兼容行为
- [x] 1.3 增加 rollout `tool_result` 类型、payload 和 helper
- [x] 1.4 更新历史读取逻辑，使 tool_result 能恢复为内部消息 block

## 2. Provider tool call 支持

- [x] 2.1 OpenAI adapter 转换 tools、assistant tool_use 与 tool_result 消息
- [x] 2.2 OpenAI streaming 聚合 tool_calls delta 并产出完整 `tool_call` 事件
- [x] 2.3 compat provider 复用 OpenAI tool call 支持并补测试
- [x] 2.4 Anthropic adapter 转换 tools、tool_use 与 tool_result 消息
- [x] 2.5 Anthropic streaming 聚合 tool_use/input_json_delta 并产出完整 `tool_call` 事件

## 3. Agent loop

- [x] 3.1 新建 `internal/agent`，定义 `Agent`、`Options`、`Result` 和 maxTurns 错误
- [x] 3.2 实现 provider stream 消费：文本拼接、usage 写入、assistant tool_use message 构造
- [x] 3.3 实现工具 dispatch：Registry 查找、PreviewableTool 预览、permission Ask、Execute、tool_result message
- [x] 3.4 实现 permission 拒绝/工具错误转错误 tool_result，确保不执行被拒绝工具
- [x] 3.5 实现 rollout 写入 user/assistant/usage/tool_result/error

## 4. CLI `ub run`

- [x] 4.1 为 `ub run` 增加 `-p/--prompt`、`--provider`、`--model` 参数
- [x] 4.2 注册 fs/search/shell/job 本地工具并构造 permission manager
- [x] 4.3 解析 `--mode`，调用 Agent 并把最终 assistant 文本写到 stdout
- [x] 4.4 保持 `ub chat` 裸聊天行为，遇到 tool_call 继续报可读错误

## 5. 单测

- [x] 5.1 fake provider 单测：tool_call 后第二轮返回最终文本
- [x] 5.2 agent 单测：fake script 调 read 后返回最终回答
- [x] 5.3 agent 单测：plan 模式 edit 被拒且文件未修改
- [x] 5.4 agent 单测：Preview 指针传给 permission Ask 且 Execute 只在 allow 后调用
- [x] 5.5 provider 单测：OpenAI tools/tool_result 转换与 streaming tool_call 聚合
- [x] 5.6 provider 单测：Anthropic tools/tool_result 转换与 streaming tool_call 聚合
- [x] 5.7 rollout 单测：tool_result helper 与历史恢复
- [x] 5.8 CLI 单测：`ub run -p` fake provider 冒烟与 `--mode plan`

## 6. 验证

- [ ] 6.1 运行 `go test ./internal/agent ./internal/provider/... ./internal/rollout ./internal/cli`
- [ ] 6.2 运行 `go test ./...`
- [x] 6.3 运行 `make lint`
- [x] 6.4 运行 `make build`
- [x] 6.5 运行 `openspec validate add-agent-loop --strict`
