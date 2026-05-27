# workspace-memory Specification (delta: add-workspace-memory)

## ADDED Requirements

### Requirement: memory 文件路径

系统 SHALL 把 memory 持久化到两个 scope 的 markdown 文件:

- `ScopeWorkspace`:`<workspace>/.ub/memory.md`
- `ScopeGlobal`:`<XDG_CONFIG_HOME 或 ~/.config>/ub/memory.md`

`memory.Path(workspaceRoot, scope) (string, error)` MUST 返回上述路径;`workspaceRoot` 为空且 scope=workspace 时 MUST 返回错误。

#### Scenario: workspace 路径

- **GIVEN** workspaceRoot = `/tmp/proj`
- **WHEN** 调用 `memory.Path("/tmp/proj", ScopeWorkspace)`
- **THEN** 返回 `/tmp/proj/.ub/memory.md`,nil

#### Scenario: global 路径 honors XDG_CONFIG_HOME

- **GIVEN** `XDG_CONFIG_HOME=/tmp/cfg`
- **WHEN** 调用 `memory.Path("", ScopeGlobal)`
- **THEN** 返回 `/tmp/cfg/ub/memory.md`,nil

#### Scenario: 空 workspaceRoot 拒绝

- **WHEN** 调用 `memory.Path("", ScopeWorkspace)`
- **THEN** 返回非 nil error

### Requirement: memory.Append 语义

`memory.Append(workspaceRoot, scope, text)` MUST 在对应文件末尾追加一段:一个空行,然后一行 H2 标题 `## <RFC3339 时间>`,空行,再然后是去掉首尾空行的 `text`,末尾换行。父目录不存在时 MUST mkdir(0o755)。文件不存在时 MUST 创建(0o644)。返回写入的文件绝对路径与本次条目的标题字符串(便于工具 result 引用)。

#### Scenario: 文件不存在时创建

- **GIVEN** `.ub/memory.md` 尚不存在
- **WHEN** 调用 `Append(ws, ScopeWorkspace, "first")`
- **THEN** `.ub/memory.md` MUST 被创建,内容包含 `## ` 标题与 `first` 段落

#### Scenario: 多次 append 顺序保持

- **GIVEN** 已经 Append 过 "first" 的 memory 文件
- **WHEN** 再次 `Append(ws, ScopeWorkspace, "second")`
- **THEN** 文件中 "first" MUST 出现在 "second" 之前

### Requirement: memory.Read 拼接与截断

`memory.Read(workspaceRoot, maxChars)` MUST 返回 global + workspace 两段拼接后的字符串,中间用 `\n---\n` 分隔,segment 头部用 HTML 注释标注:`<!-- global memory --> ... <!-- workspace memory -->`。任一文件不存在 MUST 视作空串,**不**报错。

`maxChars > 0` 时,如果拼接后总长度超过 maxChars,系统 MUST **保留尾部**(因为越靠后的条目越新),从头截掉到 ≤ maxChars,在保留段开头插入 `... [memory truncated]\n` 一行。`maxChars <= 0` 视作无截断。

#### Scenario: 拼接顺序 + 注释标记

- **GIVEN** 两段 memory 文件均非空
- **WHEN** 调用 `memory.Read(ws, 0)`
- **THEN** 返回字符串 MUST 含 `<!-- global memory -->` 与 `<!-- workspace memory -->` 注释,且 global 段 MUST 出现在 workspace 段之前

#### Scenario: 任一文件缺失视作空

- **GIVEN** workspace 文件存在,global 文件不存在
- **WHEN** 调用 `memory.Read(ws, 0)`
- **THEN** 返回字符串 MUST 只含 workspace 段,且不报错

#### Scenario: 截断保留尾部

- **GIVEN** 拼接后内容 = 10000 字符,maxChars = 200
- **WHEN** 调用 `memory.Read(ws, 200)`
- **THEN** 返回长度 ≤ 250,且 MUST 含 `... [memory truncated]` 标记;尾部最新条目 MUST 仍在返回值中

### Requirement: remember 工具

系统 SHALL 提供 `remember` 工具,`Risk` 为 `RiskSafe`。input schema MUST 含 `text: string`(必填),可选 `scope: string`(取值 `workspace`(默认)或 `global`)。Execute MUST 调用 `memory.Append`;非空 text 与合法 scope 之外的输入 MUST 在写盘前返回错误。

`Result.Content` MUST 包含写入的绝对路径与新条目的标题行;`Result.Files` MUST 含一条 `FileChange{Path: relative or absolute, Kind: "modify" or "create"}`(create 仅当本次创建文件)。

#### Scenario: workspace 写入

- **GIVEN** 一个工作区,且 `.ub/memory.md` 尚不存在
- **WHEN** 调用 `remember(text="build is `make build`")`
- **THEN** `.ub/memory.md` MUST 被创建,文件末尾 MUST 含 `## ` 开头的时间戳行 + `build is `make build`` 一段

#### Scenario: global 写入

- **GIVEN** ub 默认 config home
- **WHEN** 调用 `remember(text="prefer pnpm over npm", scope="global")`
- **THEN** `<config home>/ub/memory.md` MUST 出现一条新条目

#### Scenario: 空 text 拒绝

- **WHEN** 调用 `remember(text="")`
- **THEN** 工具 MUST 返回错误且不写盘

#### Scenario: 非法 scope 拒绝

- **WHEN** 调用 `remember(text="x", scope="session")`
- **THEN** 工具 MUST 返回错误且不写盘

### Requirement: agent 注入

系统 SHALL 在 `agent.Run` 准备发送给 provider 的 messages 时,在原有 `<environment_context>` 系统消息之后,插入一条 role=system 的消息,正文格式:

```
<workspace_memory>
<memory.Read 的结果>
</workspace_memory>
```

memory 内容为空时 MUST 不插入这条消息(避免给 model 一个无用的空块)。`Options.WorkspaceRoot` 为空时 MUST 不调用 memory.Read,不插入。

`memory.Read` 的 `maxChars` 入参 MUST 来自 `Options.MemoryMaxChars`,该字段 ≤ 0 时使用默认值 4000。

#### Scenario: 注入存在

- **GIVEN** workspace 中 `.ub/memory.md` 含一条 "build is `make build`"
- **WHEN** Agent.Run 准备 provider 请求
- **THEN** provider 收到的 messages MUST 含一条 role=system 且 text 包含 `build is `make build`` 的消息

#### Scenario: 空 memory 不注入

- **GIVEN** workspace 中没有 memory 文件
- **WHEN** Agent.Run 准备 provider 请求
- **THEN** provider 收到的 messages MUST NOT 含任何带 `<workspace_memory>` 标签的消息
