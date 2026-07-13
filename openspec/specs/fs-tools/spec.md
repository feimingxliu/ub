# fs-tools Specification

## Purpose
TBD - created by archiving change add-fs-tools. Update Purpose after archive.
## Requirements
### Requirement: Workspace 沙箱

系统 SHALL 对所有 fs 工具实施统一的路径沙箱：每次工具调用 MUST 把入参 `path` 经过 `filepath.Clean` 与 `filepath.Rel(root, abs)` 校验，若 clean 后的绝对路径不在 `fs.Register` 注入的 `root` 之下，工具 MUST 返回包含 `path is outside workspace root` 字样的错误，且 MUST NOT 读盘或写盘。绝对路径与相对路径 MUST 走同一校验函数。

#### Scenario: 拒绝相对路径跳出 root

- **GIVEN** root 注入为 `/tmp/ws`
- **WHEN** 工具收到 `path="../etc/passwd"`
- **THEN** 返回错误并不访问任何文件

#### Scenario: 拒绝绝对路径跳出 root

- **GIVEN** root 注入为 `/tmp/ws`
- **WHEN** 工具收到 `path="/etc/passwd"`
- **THEN** 返回错误并不访问任何文件

#### Scenario: 接受 root 内绝对路径

- **GIVEN** root 注入为 `/tmp/ws`，且 `/tmp/ws/a.txt` 存在
- **WHEN** 工具收到 `path="/tmp/ws/a.txt"`
- **THEN** 校验通过

### Requirement: fs.Register 入口

系统 SHALL 暴露 `fs.Register(reg *tool.Registry, root string) error` 一次性把 read / ls / glob / write / edit / multiedit / apply_patch 七个基础工具注册到给定 Registry。注册顺序无关，但完成后 `reg.All()` MUST 包含名为 `read`、`ls`、`glob`、`write`、`edit`、`multiedit`、`apply_patch` 的七个工具。`Register` 任一注册步骤失败（例如 Registry 已存在同名工具）时 MUST 立即返回该错误。

#### Scenario: 注册七个基础工具

- **GIVEN** 一个空的 Registry 与一个可写的临时 root
- **WHEN** 调用 `fs.Register(reg, root)`
- **THEN** 返回 nil 错误且 Registry 中包含 `read`、`ls`、`glob`、`write`、`edit`、`multiedit`、`apply_patch`

### Requirement: read 工具

系统 SHALL 提供 `read` 工具，`Risk` 为 `RiskSafe`。input schema MUST 包含 `path string`（必填）、`offset int`（可选，从 1 开始）、`limit int`（可选）。返回的 `Result.Content` MUST 是带行号的文本，行号宽度按本次输出的最大行号对齐；当未指定 `limit` 且文件超过 2000 行时，输出 MUST 截断到前 2000 行并附加截断提示。

#### Scenario: 读取整文件

- **GIVEN** root 中存在三行文件 `a.txt`
- **WHEN** 调用 `read(path="a.txt")`
- **THEN** `Result.Content` MUST 包含全部三行，每行以行号前缀

#### Scenario: 使用 offset/limit

- **GIVEN** root 中存在十行文件 `b.txt`
- **WHEN** 调用 `read(path="b.txt", offset=3, limit=2)`
- **THEN** `Result.Content` MUST 只包含原第 3、4 行

#### Scenario: 超大文件截断

- **GIVEN** root 中存在 3000 行文件
- **WHEN** 调用 `read(path="big.txt")`
- **THEN** `Result.Content` MUST 仅包含前 2000 行并附加截断提示文本

### Requirement: ls 工具

系统 SHALL 提供 `ls` 工具，`Risk` 为 `RiskSafe`。input schema MUST 包含 `path string`（必填，表示要列出的目录）。`Result.Content` MUST 是按条目名字典序排序的多行文本，每行格式为 `<kind>\t<name>`，`kind` 取值 `dir` / `file` / `symlink` / `other`。

#### Scenario: 列出目录

- **GIVEN** root 中存在 `sub/` 目录与 `a.txt`、`b.txt` 文件
- **WHEN** 调用 `ls(path=".")`
- **THEN** `Result.Content` MUST 包含三行：`file\ta.txt`、`file\tb.txt`、`dir\tsub`，按名字字典序排序

#### Scenario: 路径不是目录

- **GIVEN** root 中只有文件 `a.txt`
- **WHEN** 调用 `ls(path="a.txt")`
- **THEN** 工具 MUST 返回错误说明路径不是目录

### Requirement: glob 工具

系统 SHALL 提供 `glob` 工具，`Risk` 为 `RiskSafe`，匹配引擎 MUST 使用 `github.com/bmatcuk/doublestar/v4`。input schema MUST 包含 `pattern string`（必填，doublestar 表达式，相对 root）。`Result.Content` MUST 是匹配路径列表，按字符串字典序排序，每行一条，路径相对 root。

#### Scenario: 递归通配匹配

- **GIVEN** root 中存在 `a/b/c.go` 与 `a/d.go`
- **WHEN** 调用 `glob(pattern="**/*.go")`
- **THEN** `Result.Content` MUST 是按字典序排序的两行：`a/b/c.go` 和 `a/d.go`

#### Scenario: 无匹配

- **GIVEN** root 为空
- **WHEN** 调用 `glob(pattern="**/*.go")`
- **THEN** `Result.Content` MUST 为空字符串，`Result.IsError` MUST 为 false

### Requirement: write 工具

系统 SHALL 提供 `write` 工具，`Risk` 为 `RiskWrite`，且 MUST 实现 `tool.PreviewableTool`。input schema MUST 包含 `path string`（必填）和 `content string`（必填）。Execute MUST 创建缺失的父目录并以 `0o644` 覆盖写文件；执行成功后 `Result.Files` MUST 包含一条 `FileChange{Path, Kind}`，`Kind` 与 Preview 一致。

#### Scenario: 写新文件 Preview

- **GIVEN** root 中不存在 `new.txt`
- **WHEN** 调用 `Preview(path="new.txt", content="hello\n")`
- **THEN** 返回 `Preview` MUST 含一个 `FileDiff{Kind: "create", Path: "new.txt"}`，`UnifiedDiff` MUST 是空文件到新内容的合法 unified diff，且磁盘上仍不存在 `new.txt`

#### Scenario: 覆盖写已有文件 Preview

- **GIVEN** root 中存在 `a.txt`，内容为 `old\n`
- **WHEN** 调用 `Preview(path="a.txt", content="new\n")`
- **THEN** 返回 `Preview` MUST 含一个 `FileDiff{Kind: "modify"}`，`UnifiedDiff` MUST 同时包含 `-old` 与 `+new` 行，且磁盘内容仍为 `old\n`

#### Scenario: Execute 写入磁盘

- **GIVEN** root 中不存在 `dir/new.txt`
- **WHEN** 调用 `Execute(path="dir/new.txt", content="x\n")`
- **THEN** `dir/new.txt` MUST 被创建，内容为 `x\n`，`Result.Files` MUST 含 `{Path: "dir/new.txt", Kind: "create"}`

### Requirement: edit 工具

系统 SHALL 提供 `edit` 工具，`Risk` 为 `RiskWrite`，且 MUST 实现 `tool.PreviewableTool`。input schema MUST 包含 `path string`（必填）、`old string`（必填）、`new string`（必填）、`replace_all bool`（可选，默认 false）。匹配次数为 0 时 MUST 返回错误；匹配次数 > 1 且 `replace_all=false` 时 MUST 返回错误并附上匹配次数。Preview 使用 `github.com/aymanbagabas/go-udiff` 计算并返回 `Kind: "modify"`。

#### Scenario: 单匹配替换 Preview

- **GIVEN** root 中存在 `a.go`，内容为 `foo\nbar\n`
- **WHEN** 调用 `Preview(path="a.go", old="foo", new="baz")`
- **THEN** `Preview.Files[0].UnifiedDiff` MUST 包含 `-foo` 与 `+baz`，且磁盘上 `a.go` 仍为 `foo\nbar\n`

#### Scenario: 多匹配未开启 replace_all

- **GIVEN** root 中存在 `b.go`，内容为 `x\nx\n`
- **WHEN** 调用 `Preview(path="b.go", old="x", new="y")` 或 `Execute(...)`
- **THEN** 返回错误，错误消息 MUST 提示匹配次数，且不修改磁盘

#### Scenario: 多匹配开启 replace_all

- **GIVEN** root 中存在 `c.go`，内容为 `x\nx\n`
- **WHEN** 调用 `Execute(path="c.go", old="x", new="y", replace_all=true)`
- **THEN** 文件内容 MUST 变为 `y\ny\n`，`Result.Files` MUST 含一条 `Kind: "modify"`

#### Scenario: old 不存在

- **GIVEN** root 中存在 `d.go`，内容为 `hello\n`
- **WHEN** 调用 `Preview(path="d.go", old="missing", new="x")` 或 `Execute(...)`
- **THEN** 返回错误，错误消息 MUST 提示 `old string not found`

#### Scenario: Execute 时磁盘已变更

- **GIVEN** root 中 `e.go` 初始内容为 `a\n`
- **WHEN** 调用 `Execute(path="e.go", old="a", new="b")`，且在 Execute 内部读盘前文件内容已被改成 `c\n`
- **THEN** Execute MUST 返回错误说明文件已变更，且 MUST NOT 把磁盘内容覆盖成 `b\n`

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

### Requirement: tool_result 工具

系统 SHALL 提供 `tool_result` 工具,`Risk` 为 `RiskSafe`。input schema MUST 包含 `tool_use_id: string`(必填),并 MAY 包含 `offset: int`(可选,从 1 开始)与 `limit: int`(可选)。工具 MUST 从 `context.Context` 中读取由 agent runtime 注入的 sessionID。如果 ctx 中未携带 sessionID,工具 MUST 返回包含 `session id` 字样的错误且不读盘。

工具 MUST 通过 `tooloutput.SpilloverPath(stateRoot, sessionID, tool_use_id)` 推导 spillover 文件路径。当目标文件不存在时,工具 MUST 返回包含 `not found or output was not spilled` 字样的错误。文件存在时,`Result.Content` MUST 是带行号的文本,行号宽度按本次输出的最大行号对齐,默认 2000 行截断与 `read` 工具一致。

`tool_result` MUST NOT 通过 input 接受任意磁盘路径;路径 MUST 由 sessionID + tool_use_id + 注册时固定的 outputRoot 派生,以避免越权读取 spillover 目录之外的文件。

#### Scenario: 读取存在的 spillover 文件

- **GIVEN** 当前 session 的 sessionID 为 `S`,`<outputRoot>/<safe(S)>/<safe(T)>.txt` 内容为 `alpha\nbeta\ngamma\n`,且 ctx 已注入 sessionID=S
- **WHEN** 调用 `tool_result(tool_use_id="T")`
- **THEN** `Result.Content` MUST 包含全部三行,每行带行号前缀

#### Scenario: 文件缺失

- **GIVEN** spillover 路径不存在
- **WHEN** 调用 `tool_result(tool_use_id="X")`
- **THEN** 工具 MUST 返回包含 `not found or output was not spilled` 字样的错误

#### Scenario: 缺少 sessionID

- **GIVEN** ctx 中未注入 sessionID
- **WHEN** 调用 `tool_result(tool_use_id="T")`
- **THEN** 工具 MUST 返回错误且不访问任何文件

#### Scenario: offset / limit

- **GIVEN** spillover 文件有 10 行
- **WHEN** 调用 `tool_result(tool_use_id="T", offset=3, limit=2)`
- **THEN** `Result.Content` MUST 只包含原第 3、4 行

### Requirement: fs.Register 条件注册 tool_result

系统 SHALL 在调用 `fs.RegisterWithOptions` 时,仅当 `Options.OutputRoot`(或回落用的 `Options.StateRoot`)指向非空目录时才注册 `tool_result`;否则 `Register` MUST 跳过 `tool_result` 的注册并不报错。其余 7 个工具(`read`/`ls`/`glob`/`write`/`edit`/`multiedit`/`apply_patch`)不受影响。

#### Scenario: OutputRoot 提供时注册八件套

- **GIVEN** 一个空 Registry、一个临时 root 与一个临时 outputRoot
- **WHEN** 调用 `fs.RegisterWithOptions(reg, root, fs.Options{OutputRoot: outputRoot})`
- **THEN** Registry MUST 含 `read`、`ls`、`glob`、`write`、`edit`、`multiedit`、`apply_patch`、`tool_result` 共 8 个工具

#### Scenario: OutputRoot 缺失时回退七件套

- **GIVEN** 一个空 Registry 与一个临时 root
- **WHEN** 调用 `fs.Register(reg, root)`
- **THEN** Registry MUST 只含 7 个基础工具且不含 `tool_result`
