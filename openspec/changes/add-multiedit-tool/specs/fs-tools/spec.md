# fs-tools Specification (delta: add-multiedit-tool)

## ADDED Requirements

### Requirement: multiedit 工具

系统 SHALL 提供 `multiedit` 工具,`Risk` 为 `RiskWrite`,且 MUST 实现 `tool.PreviewableTool`。input schema MUST 包含 `edits: array`(必填,至少 1 条),每条 edit 的 schema 与 `edit` 工具一致:`{path: string(必填), old: string(必填), new: string(必填), replace_all: bool(可选,默认 false)}`。

`multiedit` MUST 提供原子性语义:Execute 阶段先在内存中对所有 edits 按数组顺序逐条计算结果(同一 `path` 的多条 edits 串行累加,即第 N 条看到的是前 N-1 条应用后的内容),全部计算成功后再对每个涉及文件做"当前盘内容 == 计算前读到的 before"的 TOCTOU 二次校验,最后再批量写盘。任一 edit 失败、任一 TOCTOU 校验失败、任一写盘失败 MUST 终止整次调用并返回错误;未通过写盘步骤的文件 MUST 保持原内容不变。

`Preview` MUST 在不写盘的前提下产出每个涉及文件一条 `FileDiff{Kind: "modify"}`,`UnifiedDiff` 反映该文件所有 edits 合并应用后的最终差异。

`Execute` 成功时 `Result.Content` MUST 是人类可读的汇总(包含修改的文件数与替换次数总和),`Result.Files` MUST 按文件路径字典序包含每个涉及文件的一条 `FileChange{Kind: "modify", UnifiedDiff}`。

#### Scenario: 单文件多处编辑 Preview

- **GIVEN** root 中存在 `a.go`,内容为 `foo\nbar\nbaz\n`
- **WHEN** 调用 `Preview(edits=[{path:"a.go", old:"foo", new:"FOO"}, {path:"a.go", old:"bar", new:"BAR"}])`
- **THEN** 返回 `Preview` MUST 含一条 `FileDiff{Path:"a.go", Kind:"modify"}`,`UnifiedDiff` 同时含 `+FOO` 与 `+BAR` 行,且磁盘内容仍为 `foo\nbar\nbaz\n`

#### Scenario: 多文件 Execute

- **GIVEN** root 中存在 `a.txt`(内容 `aaa\n`)与 `b.txt`(内容 `bbb\n`)
- **WHEN** 调用 `Execute(edits=[{path:"a.txt", old:"aaa", new:"AAA"}, {path:"b.txt", old:"bbb", new:"BBB"}])`
- **THEN** `a.txt` 内容变为 `AAA\n`、`b.txt` 内容变为 `BBB\n`,`Result.Files` MUST 含两条 `FileChange`,按路径字典序排列

#### Scenario: 同文件 edit 顺序敏感

- **GIVEN** root 中存在 `a.txt`,内容为 `foo\n`
- **WHEN** 调用 `Execute(edits=[{path:"a.txt", old:"foo", new:"bar"}, {path:"a.txt", old:"bar", new:"baz"}])`
- **THEN** `a.txt` 最终内容 MUST 为 `baz\n`(第二条看到第一条应用后的结果)

#### Scenario: 其中一条 edit 失败时整体回滚

- **GIVEN** root 中存在 `a.txt`(内容 `foo\n`)与 `b.txt`(内容 `bbb\n`)
- **WHEN** 调用 `Execute(edits=[{path:"a.txt", old:"foo", new:"FOO"}, {path:"b.txt", old:"NOPE", new:"x"}])`
- **THEN** 工具 MUST 返回错误,`a.txt` 与 `b.txt` 内容 MUST 与调用前完全一致

#### Scenario: 空 edits 数组拒绝

- **WHEN** 调用 `Execute(edits=[])`
- **THEN** 工具 MUST 返回包含 "at least one edit" 字样的错误

#### Scenario: TOCTOU 检测

- **GIVEN** root 中存在 `a.txt`,内容为 `foo\n`
- **WHEN** 在 Preview 之后、Execute 写盘之前,`a.txt` 被外部进程改写为 `mutated\n`
- **THEN** `Execute` MUST 返回包含 `changed on disk` 字样的错误,且 `a.txt` 内容 MUST 保留为 `mutated\n`(不被工具覆盖)
