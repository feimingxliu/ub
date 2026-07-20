## 1. 单次失败诊断

- [x] 1.1 子进程失败后 best-effort 读取隔离 rollout，保留 session、行为和指标
- [x] 1.2 增加保守的 infrastructure failure 分类及测试，区分前置协议/配置错误与普通 agent failure

## 2. Matrix 核心编排

- [x] 2.1 定义 target、稳定样本计划、raw run、skip 和 MatrixReport 类型
- [x] 2.2 实现参数校验、稳定 run identity、每 target preflight 与 infrastructure 熔断
- [x] 2.3 实现有界并发 worker、context 取消和按计划顺序输出，并测试隔离与确定性

## 3. 聚合与报告

- [x] 3.1 实现 overall/by-target/by-task 聚合、failure/context 计数和零样本安全比率
- [x] 3.2 实现 matrix 文本与 JSON 渲染，保留完整单次 report 和失败/skip 摘要
- [x] 3.3 增加聚合、渲染和失败退出测试

## 4. CLI 集成

- [x] 4.1 支持重复 `--task`、`--target provider=model`、`--repeat` 和 `--parallel`，并校验互斥与范围
- [x] 4.2 保持单 task 旧 Report 路径兼容，matrix 路径输出完整报告并正确返回退出状态
- [x] 4.3 增加 CLI 单次兼容、target 解析、matrix JSON 和无效参数测试

## 5. 文档与验证

- [x] 5.1 同步 requirements、design、roadmap 和 eval README/示例
- [x] 5.2 运行聚焦测试、全量 test/lint/build/check、git diff check 和 OpenSpec 严格校验
