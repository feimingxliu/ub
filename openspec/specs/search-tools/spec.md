# search-tools Specification

## Purpose
TBD - created by archiving change add-search-tools. Update Purpose after archive.
## Requirements
### Requirement: search.Register 入口

系统 SHALL 暴露 `search.Register(reg *tool.Registry, root string) error`，把 `grep` 工具注册到给定 Registry。注册后 `reg.Get("grep")` MUST 返回该工具实例。`Register` 不与 `fs.Register` 合并，调用方可以独立启用或禁用。

#### Scenario: 注册 grep

- **GIVEN** 一个空 Registry 与一个可读的临时 root
- **WHEN** 调用 `search.Register(reg, root)`
- **THEN** 返回 nil 错误，且 `reg.Get("grep")` MUST 返回非 nil 工具

### Requirement: grep 工具输入

系统 SHALL 提供 `grep` 工具，`Risk` 为 `RiskSafe`。input schema MUST 包含 `pattern string`（必填，Go RE2 正则）、`path string`（可选，搜索目录，默认 `.`，相对 root）、`include string`（可选，doublestar glob 过滤路径）。`pattern` 无法被 `regexp.Compile` 解析时 MUST 返回错误，不开始任何文件 I/O。

#### Scenario: 非法正则

- **WHEN** 调用 `grep(pattern="(", path=".")`
- **THEN** 返回错误，错误消息 MUST 提示正则解析失败

#### Scenario: 缺省 path

- **GIVEN** root 下存在 `a.txt`，内容包含 `hello`
- **WHEN** 调用 `grep(pattern="hello")`（不传 path）
- **THEN** 搜索从 root 开始，结果 MUST 包含 `a.txt` 的命中

### Requirement: grep 输出格式

系统 SHALL 让 `grep` 的 `Result.Content` 为每行 `path:line:match` 形式的文本。`path` MUST 是相对 root 的 POSIX 风格路径（分隔符 `/`），`line` MUST 是 1-based 整数，`match` MUST 是该行原文（保留前后空白）。多条结果 MUST 按 `path` 升序、同文件按 `line` 升序排序。无匹配时 `Content` MUST 为空字符串且 `IsError` MUST 为 false。

#### Scenario: 多匹配排序

- **GIVEN** root 下存在 `b.txt` 内容 `x\nx\n`，`a.txt` 内容 `x\n`
- **WHEN** 调用 `grep(pattern="x")`
- **THEN** `Result.Content` MUST 是 `a.txt:1:x\nb.txt:1:x\nb.txt:2:x`（末尾可选换行）

#### Scenario: 无匹配

- **GIVEN** root 下存在 `a.txt` 内容 `hello\n`
- **WHEN** 调用 `grep(pattern="world")`
- **THEN** `Result.Content` MUST 为空字符串，`Result.IsError` MUST 为 false

#### Scenario: 单行过长截断

- **GIVEN** root 下存在文件，命中行原文长度超过 2048 字节
- **WHEN** 调用 `grep` 命中该行
- **THEN** 输出中该行的 `match` 部分 MUST 在 2048 字节处截断并附加 ` ...(truncated)`

### Requirement: grep 沙箱

系统 SHALL 让 `grep` 调用 `tool.Resolve(root, path)` 校验 `path`；解析后路径若不在 root 之下，工具 MUST 返回包含 `path is outside workspace root` 字样的错误，且 MUST NOT 开始任何文件遍历。

#### Scenario: 跳出 root

- **GIVEN** root 为 `/tmp/ws`
- **WHEN** 调用 `grep(pattern="x", path="../")`
- **THEN** 返回错误，不访问 `/tmp` 下其它内容

### Requirement: grep include 过滤

系统 SHALL 在传入 `include` 时只输出与该 doublestar glob 匹配的相对 root 路径。匹配通过 `doublestar.PathMatch(include, relPath)`；不匹配的文件 MUST 不出现在 `Result.Content`。

#### Scenario: 仅匹配 go 文件

- **GIVEN** root 下存在 `a.go` 与 `b.md`，二者都含 `hello`
- **WHEN** 调用 `grep(pattern="hello", include="*.go")`
- **THEN** `Result.Content` MUST 只包含 `a.go` 的命中，不包含 `b.md`

### Requirement: grep 二进制文件跳过

系统 SHALL 在内置后端中通过读取首 8 KB 判断 NUL 字节，命中即视为二进制文件并跳过；外部 ripgrep 后端依赖 rg 自身的二进制检测。任何情况下二进制文件 MUST 不出现在 `Result.Content`。

#### Scenario: 二进制文件不进入结果

- **GIVEN** root 下存在 `bin.dat`，首字节即包含 `\x00`，文件中其它字节与 pattern 匹配
- **WHEN** 调用 `grep(pattern=".")`
- **THEN** `Result.Content` MUST 不包含 `bin.dat`

### Requirement: 后端可注入

系统 SHALL 在 `internal/tool/search` 包内提供可注入的后端选择函数，使得测试可以强制走内置 Go 实现或模拟 ripgrep 后端，且默认实现 MUST 是内置 Go 后端（不依赖系统是否安装 `rg`）。当后续 iteration 通过配置启用 ripgrep 后端时，行为差异 MUST 限定为性能与是否读取 `.gitignore`，不影响 `Result.Content` 排序与格式。

#### Scenario: 默认使用内置后端

- **GIVEN** 系统 PATH 中存在或不存在 `rg`
- **WHEN** 调用 `search.Register(reg, root)` 后通过 `grep` 工具执行任意搜索
- **THEN** 实际执行的后端 MUST 是内置 Go 实现（可通过测试注入点断言）

#### Scenario: 注入后端切换

- **GIVEN** 测试代码通过包内注入点把后端替换为 fake
- **WHEN** 调用 `grep`
- **THEN** fake 后端 MUST 接收到工具传入的 `pattern`、`path`、`include` 参数

