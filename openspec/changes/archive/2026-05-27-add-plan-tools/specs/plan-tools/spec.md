# plan-tools Specification (delta: add-plan-tools)

## ADDED Requirements

### Requirement: Plan 文件存储

系统 SHALL 把 plan 工件存放在 `<workspace>/.ub/plans/<plan_id>.md`,`plan_id` 由 `plan_write` 生成,格式为 `<RFC3339 时间戳的 yyyymmddTHHMMSSZ>-<slug>`。slug MUST 从 title 派生:仅保留 ASCII 字母数字与 `-`,其余字符替换为 `-`,连续 `-` 折叠为单个,首尾 `-` 去掉,长度截断到 40。`.ub/plans/` 目录不存在时 plan_write MUST 先创建(权限 0o755)。

#### Scenario: slug 派生

- **GIVEN** title = "Fix Login Bug!"
- **WHEN** `plan_write` 生成 plan_id
- **THEN** plan_id 的 slug 部分 MUST 等于 `fix-login-bug`(连续非字母数字被合并为单个 `-`,且去掉了尾部 `-`)

#### Scenario: 目录自动创建

- **GIVEN** workspace 中尚不存在 `.ub/plans/` 目录
- **WHEN** 调用 `plan_write(title="x", steps=["a"])`
- **THEN** `.ub/plans/` 目录 MUST 被创建,plan 文件 MUST 落在该目录下

### Requirement: Plan markdown 模板

`plan_write` 生成的 markdown MUST 满足固定结构,以保证后续 `plan_update_step` 可解析。顶部 MUST 是一个 `# <title>` 一级标题,空行后跟两行 metadata `Created: <RFC3339 时间>` 与 `Status: in_progress`。后续 MUST 依次出现 `Steps`、`Notes`、`Log` 三个二级标题段(均以 `## ` 起首)。

Steps 段下每一步为一行,格式 `- [<m>] <i>. <text>`:`<m>` 取值 `空格`(未开始)/ `x`(完成)/ `~`(跳过)/ `!`(失败);`<i>` 是 1-based 序号;`<text>` 是步骤说明。Notes 段在 `notes` 入参为空时仍 MUST 保留标题但内容为空。Log 段初始 MUST 为空,后续 `plan_update_step` 调用 MUST 把 log 行追加到该段末尾。

#### Scenario: 初始模板字段齐全

- **GIVEN** title = "T",steps = ["a", "b"],notes = "n"
- **WHEN** plan 文件刚被 plan_write 生成
- **THEN** 文件 MUST 同时包含 `# T` 一级标题、`Status: in_progress` metadata、两条以 `- [ ] N. ` 开头的 step 行、Notes 段(含 `n`)、Log 段(空)

### Requirement: plan_write 工具

系统 SHALL 提供 `plan_write` 工具,`Risk` 为 `RiskSafe`。input schema MUST 含 `title: string`(必填)、`steps: []string`(必填,至少 1 条)、可选 `notes: string`。空 title 或空 steps 数组 MUST 返回错误并不写盘。生成的 plan_id 与目标路径冲突时(已存在同名文件)MUST 返回错误并不覆盖现有文件。Execute 成功后 `Result.Content` MUST 包含 plan_id、绝对路径与完整初始 markdown;`Result.Files` MUST 含一条 `{Path:".ub/plans/<id>.md", Kind:"create"}`。

#### Scenario: 写新 plan

- **GIVEN** workspace 中尚无 `.ub/plans/` 目录
- **WHEN** 调用 `plan_write(title="Fix login bug", steps=["repro","patch","test"])`
- **THEN** 生成的 `<workspace>/.ub/plans/<id>.md` MUST 存在,内容 MUST 同时包含 `# Fix login bug`、`Status: in_progress`、`- [ ] 1. repro`、`- [ ] 2. patch`、`- [ ] 3. test`、`## Notes`、`## Log`

#### Scenario: 空 steps 拒绝

- **WHEN** 调用 `plan_write(title="x", steps=[])`
- **THEN** 工具 MUST 返回包含 `steps` 字样的错误且 `.ub/plans/` 中没有新文件

### Requirement: plan_update_step 工具

系统 SHALL 提供 `plan_update_step` 工具,`Risk` 为 `RiskSafe`。input schema MUST 含 `plan_id: string`(必填)、`step_index: int`(必填,1-based)、`status: string`(必填,取值 `done`/`skipped`/`failed`,以及 `pending` 作为回退到未开始的可选状态)、可选 `note: string`。

Execute MUST:

1. 拼出路径 `<workspace>/.ub/plans/<plan_id>.md`,若不存在返回错误
2. 解析现有内容,验证 `step_index` 在 `[1, len(steps)]` 范围内
3. 将对应步骤行的 checkbox 改成对应字符:`done`→`x`,`skipped`→`~`,`failed`→`!`,`pending`→` `
4. 在 `## Log` section 末尾追加一行 `- <RFC3339 现在时间> step <i> → <status>[: <note>]`
5. 当 Steps 中不再有 `[ ]` 时,把 metadata block 中的 `Status:` 行改为 `Status: complete`;否则保持 `in_progress`
6. 原子写回(temp file + rename)

`Result.Content` MUST 含更新后的 Steps section 文本与 metadata.Status;`Result.Files` MUST 含一条 `{Kind:"modify"}` 的 FileChange。

#### Scenario: 标记完成

- **GIVEN** 一个已存在的 plan 含三个未完成 step
- **WHEN** 调用 `plan_update_step(plan_id="...", step_index=2, status="done", note="patched")`
- **THEN** 文件中第二行 MUST 由 `- [ ] 2. ...` 变为 `- [x] 2. ...`;`## Log` 末尾 MUST 增加一行包含 `step 2 → done: patched`;metadata `Status:` MUST 保持 `in_progress`(因为还有 step 未完成)

#### Scenario: 自动 complete

- **GIVEN** plan 中最后一个未完成 step 是 step 3
- **WHEN** 调用 `plan_update_step(plan_id="...", step_index=3, status="done")`
- **THEN** metadata `Status:` MUST 变为 `complete`

#### Scenario: 越界 step_index

- **WHEN** plan 只有 3 步,调用 `step_index=5, status=done`
- **THEN** 工具 MUST 返回错误且文件不变

#### Scenario: 文件不存在

- **WHEN** `plan_id` 对应的文件不存在
- **THEN** 工具 MUST 返回包含 `not found` 字样的错误且不写盘

### Requirement: plan.Register

系统 SHALL 暴露 `plan.Register(reg *tool.Registry, workspaceRoot string) error`,在 `workspaceRoot` 非空时把上述两个工具注册到 Registry。`workspaceRoot` 为空时 MUST 返回错误。

#### Scenario: 注册两个工具

- **GIVEN** 一个空 Registry 与一个临时 workspace
- **WHEN** 调用 `plan.Register(reg, workspace)`
- **THEN** Registry MUST 含 `plan_write` 与 `plan_update_step`
