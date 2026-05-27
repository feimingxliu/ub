# fs-tools Specification (delta: add-tool-result-snapshot)

## ADDED Requirements

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

系统 SHALL 在调用 `fs.RegisterWithOptions` 时,仅当 `Options.OutputRoot`(或回落用的 `Options.StateRoot`)指向非空目录时才注册 `tool_result`;否则 `Register` MUST 跳过 `tool_result` 的注册并不报错。其余 6 个工具(`read`/`ls`/`glob`/`write`/`edit`/`multiedit`)不受影响。

#### Scenario: OutputRoot 提供时注册七件套

- **GIVEN** 一个空 Registry、一个临时 root 与一个临时 outputRoot
- **WHEN** 调用 `fs.RegisterWithOptions(reg, root, fs.Options{OutputRoot: outputRoot})`
- **THEN** Registry MUST 含 `read`、`ls`、`glob`、`write`、`edit`、`multiedit`、`tool_result` 共 7 个工具

#### Scenario: OutputRoot 缺失时回退六件套

- **GIVEN** 一个空 Registry 与一个临时 root
- **WHEN** 调用 `fs.Register(reg, root)`
- **THEN** Registry MUST 只含 6 个工具且不含 `tool_result`
