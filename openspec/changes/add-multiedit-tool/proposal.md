## Why

`docs/design.md` 在 V1 阶段就声明过 `multiedit`(单次调用做多个文件的多处编辑),但实际未实现。`roadmap-v2.md` §3-05 把它列为 V2 阶段工程量 S 的优先项。引入它的价值有两点:

1. **减少 round trip**:agent 跨文件批量重构时,不必把每处替换拆成独立 `edit` tool call,既省 turn 也降低中途某步上下文窗口溢出导致状态错乱的风险
2. **原子性语义**:同一组逻辑相关的修改要么全部生效要么全部回滚,避免出现"前两处改了,第三处失败"的中间态

## What Changes

- 新增 `internal/tool/fs/multiedit.go`,提供 `multiedit` 工具,实现 `PreviewableTool`
- input schema:`{edits: [{path, old, new, replace_all?}]}`,至少 1 条
- 复用 `edit.go` 中的 `applyEdit` 与 `resolve`,不引入新依赖
- **原子性**:Execute 一次读盘全部 before 内容,在内存里依次应用所有 edits(同一文件多次 edit 串行累加),全部成功后再做"二次读盘 == before"的 TOCTOU 校验,最后批量写。任一阶段失败 MUST 不写盘且返回明确错误,**不需要实际回滚**(因为成功前不写盘)
- **Preview**:对每个目标文件返回一条 `FileDiff{Kind: modify, UnifiedDiff}`,UnifiedDiff 反映该文件所有 edits 合并后的最终 diff
- 同一文件多个 edits 串行累加:即第 N 条 edit 看到的是前 N-1 条 edit 应用后的内容
- 注册到 `fs.Register`
- 单测覆盖:单文件多编辑、多文件、TOCTOU 命中、edit 顺序依赖、零长 edits 拒绝、子项缺失 path/old 拒绝、其中一条 old 不匹配整体失败

## Capabilities

### Modified Capabilities

- `fs-tools`:新增 `multiedit` 工具规格;`fs.Register` 注册的工具集从 5 个变为 6 个

## Impact

- 新增 `internal/tool/fs/multiedit.go` 与 `multiedit_test.go`
- 修改 `internal/tool/fs/register.go` 加入 multiedit
- 修改 `internal/tool/fs/register_test.go` 断言新增的工具名
- 不引入新依赖
- 不改动 cli / provider / rollout / config 等其他包
- 更新 `docs/design.md` 中"multiedit 计划中,未实现"相关文案
