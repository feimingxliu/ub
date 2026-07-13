## Context

`edit` / `multiedit` 当前以精确子串和可选行号范围表达变更。精确替换会因模型从编号读取结果复原 tab、行尾和大块上下文时失败；行号范围虽降低了空白复原难度，但在读取之后文件发生偏移时无法表达位置语义。现有 preview、权限、TOCTOU、统一 diff、LSP `didChange` 和文件 checkpoint 已构成可靠写入管线，重构应复用它们而非绕过。

参考 oh-my-pi、opencode 的编辑模式，本次采用 `*** Begin Patch` 信封：一段文本可描述多文件操作，每个更新 hunk 自带未改动的上下文。与其复制其可选的模糊匹配、流式协议或 Tree-sitter block 解析，本项目先实现一个 Go 原生、严格确定性的子集。

## Goals / Non-Goals

**Goals:**

- 为复杂或跨文件变更提供比 `old/new` 更容易生成且定位更稳定的主编辑协议。
- 在 Preview 和 Execute 中使用同一内存计划；对每个 hunk 的上下文只接受唯一精确匹配，拒绝猜测性写入。
- 支持新增、更新、删除、重命名，并让整段补丁复用现有的权限预览、文件变更结果、checkpoint 与 LSP 通知。
- 保持 `write`、`edit`、`multiedit` 的名称、schema 和行为不变，使现有 provider 与会话继续可用。

**Non-Goals:**

- 不引入通用 diff 库、git 命令、模糊匹配、语言特定语法保护或自动格式化。
- 不改变权限策略，不把补丁工具升级为 provider 专用 freeform/custom tool。
- 不承诺跨文件的崩溃一致性；本次保证一次工具调用的预检、TOCTOU 和写入失败回滚语义。

## Decisions

### 1. 新增独立 `apply_patch`，而非修改 `edit` 的 JSON schema

工具输入为 `{ "patch": "*** Begin Patch ... *** End Patch" }`，Risk 为 `RiskWrite` 并实现 `PreviewableTool`。信封支持 `*** Add File:`、`*** Update File:`、`*** Delete File:` 与紧随 Update 的可选 `*** Move to:`；Update 中用一个或多个 `@@` hunk，行首空格、`-`、`+` 分别表示保留、删除、新增。

选择独立工具可让模型不必在 JSON 字符串中转义大段代码，也不会让已有 `edit` 调用因互斥字段或不同错误语义而失效。未采用“让 `edit` 自动切换多种模式”，因为 tool schema 仍会向模型展示脆弱的 `old/new` 主接口，且诊断、checkpoint 和兼容边界更难维护。

### 2. 严格上下文定位，错误优先于猜测

解析器保留输入路径与 hunk 行；更新时将上下文行和删除行组成 expected 序列，在当前内存文件中按行精确搜索。每个 hunk 必须恰好匹配一次；`@@ <anchor>` 仅用于从指定锚点之后有序搜索，不放宽内容匹配。找不到或找到多个位置时，错误须包含文件、hunk 序号以及可重读/补充上下文的提示。`CRLF` 与 `LF` 按逻辑行比较，写回时保留原文件的 BOM、主导换行风格和末尾换行状态。

不采用 oh-my-pi 的 fuzzy/trim/indent 回退：它能减少失败重试，却会在重复代码或缩进变化时扩大误写范围，和本次“降低编辑错误率”的安全目标相冲突。

### 3. 先全量预演，再提交并回滚

`plan` 先解析完整信封，校验所有源/目标路径均在 workspace、操作路径不冲突、Add 目标不存在、Update/Delete 源存在且为普通文件、Move 目标不存在。它读取每个原始文件一次，按补丁顺序在内存中生成 before/after 快照与 unified diff；Preview 仅返回这些结果。

Preview 生成的计划按 `tool_use_id` 缓存在工具实例中；Execute 必须消费同一计划并在任何写入前重读全部依赖路径：原文件内容必须仍等于 Preview 的 before，新增/移动目标仍必须不存在。验证成功后按稳定路径顺序提交。若任一步失败，按快照反向恢复已变更的文件（包括删除新增文件和还原删除/移动源），错误明确标出回滚是否失败。这样用户批准的 diff 与实际写入绑定，而非重新计算一个未展示的 diff。

所有补丁 I/O 通过 Go 的 `os.Root` 约束在 workspace 根目录内，以防路径组件中的 symlink 跳到 workspace 外。单文件写入使用同目录临时文件、`Sync`、显式 `Chmod` 和 root 内 `Rename`，避免失败时先截断原文件；文件权限沿用已有文件的完整 permission bits，新文件使用 `0o644`。成功后只对仍存在的新增/更新/移动目标发 `DidChangeFile`，通知失败作为结果附注而不伪造写入失败。

### 4. 将新工具接入可见性与可恢复性边界

注册器、活动摘要、TUI diff 着色与文件 checkpoint 都识别 `apply_patch`。checkpoint 从同一严格补丁解析器提取 Add、Update、Delete 的源路径和 Move 的源/目标路径，解析失败时不执行工具且不产生不完整 checkpoint。产品文档把 `apply_patch` 列为复杂编辑的首选；`edit` / `multiedit` 保留用于小型精确替换及兼容调用。LSP rename 提示改为优先引导 `apply_patch`，仍不直接写盘。

## Risks / Trade-offs

- [严格唯一匹配仍会拒绝重复代码中的短上下文] → 返回匹配行信息并提示增加未修改上下文；这比猜测性修改安全。
- [多文件写入途中进程崩溃无法靠内存回滚] → 维持现有工具的失败回滚边界；后续若需要崩溃一致性再评估 journal/rename 事务。
- [补丁语法对模型是新接口] → 在描述、usage 和错误中提供简短格式示例，保留旧工具作为回退。
- [删除/移动没有标准 LSP didDelete 通知] → 只对存在的最终文件发 `didChange`；本次不扩展 LSP notifier 接口。

## Migration Plan

1. 增加解析、预演与执行实现及单元测试，不改变现有工具。
2. 注册并接入活动、checkpoint、TUI diff 与 LSP 通知。
3. 更新主规格和用户文档，让新会话优先看到 `apply_patch`。
4. 若回归出现，取消注册 `apply_patch` 即可恢复旧编辑路径；现有工具与文件格式无需迁移。

## Open Questions

- 无。本次将补丁输入固定为 JSON `patch` 字段；若未来 provider 支持可靠的 custom/freeform 调用，可在保持解析器不变的前提下单独增加 transport 适配。
