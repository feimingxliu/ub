## 1. Task schema 与报告

- [x] 1.1 为 eval task 增加强类型 runtime overrides，并校验窗口、触发比例和 recent-turn 范围
- [x] 1.2 在 JSON/文本报告中输出规范化后的 runtime overrides

## 2. 隔离运行时传播

- [x] 2.1 为 headless run 增加隐藏的 context override 参数，并在 provider/model 解析后应用到当前进程
- [x] 2.2 Eval runner 向每个 prompt/follow-up 传播相同覆盖并移除无效 `UB_EVAL`
- [x] 2.3 增加 runner 与 command 测试，覆盖参数传播、普通运行不受影响和无效值拒绝

## 3. 确定性 compact task 与文档

- [x] 3.1 更新 `compact-continuation` 的 runtime 前置条件及内置 task 集合测试
- [x] 3.2 同步 requirements、design、roadmap 和 eval task README

## 4. 验证

- [x] 4.1 运行 eval/command 聚焦测试、全量 Go 测试、lint、build 和 OpenSpec 严格校验
