## 1. Rollout 与摘要模板

- [x] 1.1 在 rollout 事件模型中新增 `summary` 类型、payload 和 helper
- [x] 1.2 让 `MessageFromEvent` 把 summary 事件恢复为 system message
- [x] 1.3 添加内嵌 summary prompt 模板

## 2. Agent 上下文准备

- [x] 2.1 为 Agent 增加 summary provider、summary model 和 context 配置选项
- [x] 2.2 在主 provider 请求前估算 token 比例并触发 summary
- [x] 2.3 实现早期历史压缩为 system summary、保留最近 N 个 user turn
- [x] 2.4 在 usage 事件里回灌 token 估算校正

## 3. CLI/TUI 接入

- [x] 3.1 `ub run` 注入 summary provider/model/context 配置
- [x] 3.2 TUI runner 注入 summary provider/model/context 配置

## 4. 验证与收尾

- [x] 4.1 添加 rollout summary helper 与历史恢复单元测试
- [x] 4.2 添加 Agent 自动 summary 触发、未触发、small_model 和失败路径单元测试
- [x] 4.3 运行 `go test ./...`
