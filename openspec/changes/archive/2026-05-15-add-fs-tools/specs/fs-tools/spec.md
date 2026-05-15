## ADDED Requirements

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

系统 SHALL 暴露 `fs.Register(reg *tool.Registry, root string) error` 一次性把 read / ls / glob / write / edit 五个工具注册到给定 Registry。注册顺序无关，但完成后 `reg.All()` MUST 包含名为 `read`、`ls`、`glob`、`write`、`edit` 的五个工具。`Register` 任一注册步骤失败（例如 Registry 已存在同名工具）时 MUST 立即返回该错误。

#### Scenario: 注册五个工具

- **GIVEN** 一个空的 Registry 与一个可写的临时 root
- **WHEN** 调用 `fs.Register(reg, root)`
- **THEN** 返回 nil 错误且 Registry 中包含 `read`、`ls`、`glob`、`write`、`edit`

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
