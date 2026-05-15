## 1. execution 包

- [x] 1.1 新建 `internal/execution/` 包，定义 `Mode`、三种常量与 `ParseMode`
- [x] 1.2 实现 `Gate(mode, risk)`，覆盖 `plan + RiskWrite` 拒绝与 exec 继续审批
- [x] 1.3 增加 `execution` 单测覆盖空模式、未知模式、write gate、exec gate

## 2. approval 包

- [x] 2.1 新建 `internal/approval/` 包，定义 `Decision`（allow/deny/unsure）、`Request`、`Result`、`Agent` 接口
- [x] 2.2 增加 `approval` 单测或编译期接口测试，确保类型可序列化/可引用

## 3. permission 数据模型

- [x] 3.1 新建 `internal/permission/` 包，定义 `Decision` 五种值、`Source`、`Request`、`Result`、`Asker`
- [x] 3.2 定义 `Rule`、`ruleFile`、命令提取与 rule match 逻辑
- [x] 3.3 实现黑名单正则匹配，覆盖三类高危命令

## 4. permission 持久化

- [x] 4.1 实现 `DefaultRulesPath()`、`LoadGlobalRules(path)`、`SaveGlobalRule(path, rule)`
- [x] 4.2 使用临时文件 + rename 原子写，保留一个可测试的写入 helper
- [x] 4.3 增加持久化测试：不存在文件、保存后加载、rename 前失败不破坏旧文件

## 5. permission Manager

- [x] 5.1 实现 `Manager`、`Options`、`NewManager`，加载 global rules 并保存 human/approval agent 依赖
- [x] 5.2 实现 `Ask(ctx, req)` 的 mode gate、safe/write 默认放行、exec 审批路径
- [x] 5.3 实现 human `Allow` / `Deny` / `AlwaysCmd` / `AlwaysTool` / `AlwaysGlobal` 决策处理
- [x] 5.4 实现 session rules 与 global rules 自动命中
- [x] 5.5 实现 `agent-approve` 模式下 approval agent allow 直放、deny/unsure/error 回退 human
- [x] 5.6 实现黑名单优先级：绕过 rule 与 approval agent，强制 human Asker
- [x] 5.7 保证 human Asker 收到原始 preview 指针

## 6. 单测

- [x] 6.1 单测：五种 Decision 路径
- [x] 6.2 单测：`plan` 模式拒绝 write 且不调用 Asker
- [x] 6.3 单测：`default` 模式 exec 未命中规则时调用 human Asker
- [x] 6.4 单测：`agent-approve` allow 不问用户，deny/unsure/error 回退 human
- [x] 6.5 单测：黑名单即使命中 global rule 也调用 human
- [x] 6.6 单测：黑名单绕过 approval agent
- [x] 6.7 单测：AlwaysGlobal 跨 Manager 生效
- [x] 6.8 单测：preview 透传给 Asker

## 7. 验证

- [x] 7.1 运行 `go test ./internal/execution ./internal/approval ./internal/permission`
- [x] 7.2 运行 `go test ./...`
- [x] 7.3 运行 `make lint`
- [x] 7.4 运行 `make build`
- [x] 7.5 运行 `openspec validate add-permission-manager --strict`
