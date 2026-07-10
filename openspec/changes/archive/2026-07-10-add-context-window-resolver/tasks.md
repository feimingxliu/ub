## 1. Resolver 与缓存

- [x] 1.1 实现 `internal/context` 的 key 规范化、候选优先级、source/confidence 与 usage/overflow 观察合并
- [x] 1.2 实现按 key 隔离的 XDG JSON 文件 store、原子写入和敏感 endpoint 清理
- [x] 1.3 添加 resolver、overflow 数值解析、冲突观察、缓存隔离和损坏缓存的单元测试

## 2. Agent 与 CLI 接入

- [x] 2.1 为 Agent Options/Factory 接入共享 resolver，并让 summary 阈值、manual compact、context event 使用动态解析结果
- [x] 2.2 在主 provider usage 和所有 context overflow recovery 入口回灌 resolver，保存失败只记录 warning
- [x] 2.3 在 headless chat、goal 和 TUI/model switch 创建按 provider endpoint/model 隔离的默认 resolver，并保持旧整数窗口回退
- [x] 2.4 扩展 Agent/TUI context event 的窗口 source/confidence 元数据及相关测试

## 3. 文档与验证

- [x] 3.1 更新 requirements/design/roadmap-v2/usage，说明窗口解析优先级、派生缓存路径和安全降级
- [x] 3.2 运行 focused tests、repo-wide tests、lint、build/check 与 `git diff --check`
