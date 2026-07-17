## 1. Task 模型与加载

- [x] 1.1 新增 `internal/eval` task/assertion/report 类型、YAML loader 和字段校验
- [x] 1.2 实现 task 名称/路径解析、fixture 安全复制和路径逃逸/symlink 防护测试

## 2. 隔离运行与判定

- [x] 2.1 实现可注入的子进程 runner，以临时 workspace、隔离 XDG state 和当前 executable 调用 `ub run`
- [x] 2.2 从隔离 session/rollout 汇总 turn、usage、工具序列、ContextDecision 和最终 assistant 文本
- [x] 2.3 实现文件、命令、工具序列/任一工具、assistant 与 context action 断言和失败分类

## 3. CLI 与报告

- [x] 3.1 接入 `ub eval --task` 及 provider/model/timeout/json/keep-workspace 参数和退出语义
- [x] 3.2 实现稳定的文本/JSON 报告，并补 CLI/runner/renderer 测试

## 4. MVP 任务与文档

- [x] 4.1 在 `docs/eval-tasks/` 增加五个 roadmap 行为任务、最小 fixture 和集合校验测试
- [x] 4.2 更新 requirements/design/roadmap/usage 与 OpenSpec 主规格，确保接口和实现一致

## 5. 验证

- [x] 5.1 运行 gofmt、目标包测试、`go test ./...`、`make lint`、`make build`、`make check` 和 `git diff --check`
- [x] 5.2 严格验证 `add-eval-mvp` OpenSpec change 并确认所有任务完成
