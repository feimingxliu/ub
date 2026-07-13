# apply-patch-tool Specification

## Purpose

定义以严格上下文补丁安全、原子地修改多个 workspace 文件的 `apply_patch` 工具。

## Requirements

### Requirement: apply_patch 工具与补丁信封

系统 SHALL 提供名为 `apply_patch` 的 `RiskWrite` 工具，并实现 `tool.PreviewableTool`。input schema MUST 包含必填的 `patch: string`。`patch` MUST 是完整的 `*** Begin Patch` / `*** End Patch` 信封，并至少含一个文件操作。每个操作 MUST 是 `*** Add File: <path>`、`*** Update File: <path>` 或 `*** Delete File: <path>`；Update 后 MAY 紧随一个 `*** Move to: <path>`。Add 的内容行 MUST 以 `+` 开头；Update MUST 含至少一个 `@@` hunk，hunk 行只允许以空格、`-`、`+` 开头，`@@` 后的可选文本是搜索锚点。空信封、未知指令、缺失路径、无 hunk 的 Update 或不合法行 MUST 返回错误且不得读写目标文件。

#### Scenario: 接受跨文件补丁

- **WHEN** 调用 `apply_patch` 并提供一个含 Add、Update 与 Delete 段的合法信封
- **THEN** 工具 MUST 解析出全部文件操作并进入统一预演流程

#### Scenario: 拒绝空补丁

- **WHEN** 调用 `apply_patch(patch="*** Begin Patch\n*** End Patch")`
- **THEN** 工具 MUST 返回含 `empty patch` 的错误且不得修改 workspace

### Requirement: 上下文唯一匹配

对每个 Update hunk，系统 MUST 将空格上下文行与 `-` 删除行按顺序作为 expected 行序列，并在该文件当前的内存内容中查找。匹配必须逐行精确且恰好唯一；`@@ <anchor>` 仅限制有序搜索起点，MUST NOT 放宽 expected 内容匹配。找不到、存在多个匹配、hunk 没有 expected 行且没有合法 EOF 定位时，工具 MUST 返回包含 path、hunk 序号和 `re-read` 或 `context` 提示的错误，且不得写入任何文件。系统 MUST NOT 使用 trim、空白归一化、缩进推断或模糊匹配来选择写入位置。

#### Scenario: 用上下文定位重复变更文本

- **GIVEN** `a.go` 中有两处文本 `return err`，但只有一处与 hunk 的前后上下文连续匹配
- **WHEN** 调用 `apply_patch` 更新该 hunk
- **THEN** 工具 MUST 只修改唯一上下文位置

#### Scenario: 拒绝歧义位置

- **GIVEN** `a.go` 中有两处都与 hunk expected 行序列完全匹配
- **WHEN** 调用 `apply_patch` 更新该 hunk
- **THEN** 工具 MUST 返回含 `multiple matches` 的错误且 `a.go` 保持不变

### Requirement: 原子预演与提交

系统 MUST 在 Preview 中完整解析、校验并在内存中应用整段补丁；当同一 `tool_use_id` 的 Execute 紧随 Preview 时，Execute MUST 使用该已验证计划，否则 MUST 重新完成解析、校验和内存应用。所有源路径、Add 目标和 Move 目标 MUST 通过 workspace 路径沙箱；Add 与 Move 目标 MUST 在预演和写入前均不存在；Update 与 Delete 源 MUST 存在且为普通文件；同一调用中冲突的源/目标路径 MUST 被拒绝。Preview MUST 返回每个最终文件一条 `FileDiff`，新增、修改、删除分别使用 `create`、`modify`、`delete` Kind，且不得写盘。

Execute MUST 在任何写入前重新校验所有已读文件仍与 before 快照相等、所有新目标仍不存在。校验失败时 MUST 返回含 `changed on disk` 或目标已存在的错误且不得写入。写入失败时 MUST 反向恢复本调用已变更的文件；成功时 `Result.Files` MUST 按最终显示路径字典序列出实际变更与 unified diff。

#### Scenario: Preview 不修改磁盘

- **GIVEN** `a.txt` 内容为 `old\n`
- **WHEN** 对 `a.txt` 调用 `apply_patch` 的 Update Preview，把 `old` 改为 `new`
- **THEN** Preview MUST 返回含 `-old` 与 `+new` 的 modify diff，且 `a.txt` 仍为 `old\n`

#### Scenario: 后续 hunk 失败时不发生部分写入

- **GIVEN** 补丁先更新 `a.txt`，随后对 `b.txt` 使用找不到的 expected 行
- **WHEN** 调用 `apply_patch`
- **THEN** 工具 MUST 返回错误，`a.txt` 与 `b.txt` 均保持调用前内容

#### Scenario: 审批后文件被外部修改

- **GIVEN** `apply_patch` 已生成 Preview 且该 tool_use_id 正在等待权限决定
- **WHEN** 用户允许前或允许后、Execute 前目标文件被外部进程修改
- **THEN** Execute MUST 返回含 `changed on disk since preview` 的错误，不得写入任何目标文件

#### Scenario: Update 与 Move

- **GIVEN** `old/name.txt` 存在，`new/name.txt` 不存在
- **WHEN** Update 段含 `*** Move to: new/name.txt` 并成功应用 hunk
- **THEN** `old/name.txt` MUST 不存在，`new/name.txt` MUST 含更新后内容，且 Result.Files MUST 报告 `new/name.txt` 的 modify 变更

#### Scenario: 拒绝指向 workspace 外的 symlink

- **GIVEN** workspace 内某个路径组件是指向 workspace 外的 symlink
- **WHEN** 补丁以该组件作为 Add、Update、Delete 或 Move 的源或目标
- **THEN** 工具 MUST 返回包含 `outside workspace root` 的错误，并且不得读取、写入或删除 symlink 指向的外部文件

#### Scenario: Move 保留源 mode

- **GIVEN** Move 的源文件 mode 为 `0o666`
- **WHEN** Update 与 Move 成功提交
- **THEN** 移动后的目标文件 mode MUST 仍为 `0o666`，不受进程 umask 降级

### Requirement: 编辑管线集成

`apply_patch` 成功修改后 MUST 对所有仍存在的最终文件调用既有 `ChangeNotifier`；通知失败 MUST 附加到 Result.Content 而不得回滚已成功写入。活动摘要和 TUI MUST 以“应用补丁”展示该工具，并把其文件详情按 unified diff 着色。文件历史 MUST 在 Execute 前从同一补丁语法提取 Add、Update、Delete 和 Move 的全部受影响路径并备份，确保 rewind 能恢复新增、删除和重命名涉及的文件。

#### Scenario: 多文件补丁被 checkpoint 追踪

- **GIVEN** 当前 turn 的文件历史 snapshot 已创建，补丁会新增 `new.txt`、删除 `old.txt`、并移动 `a.txt`
- **WHEN** `apply_patch` 通过权限并开始 Execute
- **THEN** snapshot MUST 在写入前记录 `new.txt`、`old.txt`、`a.txt` 及移动目标的调用前状态
