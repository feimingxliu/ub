## MODIFIED Requirements

### Requirement: fs.Register 入口

系统 SHALL 暴露 `fs.Register(reg *tool.Registry, root string) error` 一次性把 read / ls / glob / write / edit / multiedit / apply_patch 七个基础工具注册到给定 Registry。注册顺序无关，但完成后 `reg.All()` MUST 包含名为 `read`、`ls`、`glob`、`write`、`edit`、`multiedit`、`apply_patch` 的七个工具。`Register` 任一注册步骤失败（例如 Registry 已存在同名工具）时 MUST 立即返回该错误。

#### Scenario: 注册七个基础工具

- **GIVEN** 一个空的 Registry 与一个可写的临时 root
- **WHEN** 调用 `fs.Register(reg, root)`
- **THEN** 返回 nil 错误且 Registry 中包含 `read`、`ls`、`glob`、`write`、`edit`、`multiedit`、`apply_patch`

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
