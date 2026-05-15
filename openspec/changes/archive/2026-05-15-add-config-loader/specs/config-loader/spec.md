## ADDED Requirements

### Requirement: 解析 YAML 配置

系统 SHALL 使用 YAML 作为唯一的配置文件格式，并 MUST 拒绝非法 YAML。系统 MUST NOT 支持 JSON、TOML、INI 等其他配置语言作为输入。

#### Scenario: 合法 YAML 加载成功

- **WHEN** 用户在 `~/.config/ub/config.yaml` 写入合法 YAML（例如 `default_model: anthropic/claude-sonnet-4-7`）
- **THEN** `config.Load()` 返回的 `Config.DefaultModel` 字段等于 `anthropic/claude-sonnet-4-7`，且无错误

#### Scenario: 非法 YAML 报错

- **WHEN** 用户在 `~/.config/ub/config.yaml` 写入非法 YAML（缺冒号、不平衡缩进等）
- **THEN** `config.Load()` 返回 error，error 信息 MUST 包含文件路径和近似行号

### Requirement: 分层合并

系统 SHALL 按"内置默认 → 全局（`~/.config/ub/config.yaml`）→ 本地（当前工作目录向上最多 5 层找到的第一个 `.ub/config.yaml`）→ 环境变量覆盖"的顺序加载并合并配置。后加载层 MUST 整字段覆盖（map 按 key 替换 value、slice 整体替换、标量按非零值覆盖）。系统 MUST 容忍任一层缺失，不视为错误。

#### Scenario: 本地覆盖全局

- **WHEN** 全局配置含 `default_model: openai/gpt-4o`，本地 `.ub/config.yaml` 含 `default_model: anthropic/claude-sonnet-4-7`
- **THEN** 最终 `Config.DefaultModel` 等于 `anthropic/claude-sonnet-4-7`

#### Scenario: providers map 按 key 替换 value

- **WHEN** 全局配置含 `providers.openai = {api_key: A, base_url: B}`，本地配置含 `providers.openai = {base_url: C}`
- **THEN** 最终 `Config.Providers["openai"]` 的 `APIKey` 为空字符串，`BaseURL` 为 `C`（不做 value 内字段合并）

#### Scenario: 任一层缺失不报错

- **WHEN** 当前工作目录及其向上 5 层均无 `.ub/config.yaml`，且 `~/.config/ub/config.yaml` 不存在
- **THEN** `config.Load()` 不返回 error，返回的 `Config` 等于内置默认值

#### Scenario: 向上 5 层内找到的第一个 `.ub/` 生效

- **WHEN** 当前工作目录为 `/a/b/c/d/e`，`/a/b/.ub/config.yaml` 存在，`/a/.ub/config.yaml` 也存在
- **THEN** `config.Load()` 使用 `/a/b/.ub/config.yaml`，不使用 `/a/.ub/config.yaml`

### Requirement: 环境变量替换

系统 SHALL 在 YAML 解析前对原始字节流中所有 `${VAR}` 与 `${VAR:-default}` 形式的占位符做替换。变量名 MUST 满足 `[A-Z_][A-Z0-9_]*` 模式。系统 MUST 支持 `$$` 转义为字面 `$`。

#### Scenario: 简单替换

- **GIVEN** 环境变量 `ANTHROPIC_API_KEY=sk-abc`
- **WHEN** YAML 含 `api_key: ${ANTHROPIC_API_KEY}`
- **THEN** 加载后 `Config.Providers["anthropic"].APIKey` 等于 `sk-abc`

#### Scenario: 不存在的变量

- **GIVEN** 环境变量 `MISSING_KEY` 未设置
- **WHEN** YAML 含 `api_key: ${MISSING_KEY}`
- **THEN** 加载后对应字段为空字符串，且系统 MUST 在日志中 WARN

#### Scenario: 默认值回退

- **GIVEN** 环境变量 `MISSING_KEY` 未设置
- **WHEN** YAML 含 `base_url: ${MISSING_KEY:-http://localhost:11434}`
- **THEN** 加载后对应字段等于 `http://localhost:11434`

#### Scenario: `$$` 转义

- **WHEN** YAML 含 `prompt: "cost $$5 per call"`
- **THEN** 加载后对应字段等于字面 `cost $5 per call`，不触发任何替换

### Requirement: 内置默认值

`config.Load()` 在用户没有任何配置文件时 MUST 返回一份可直接使用的默认 `Config`，包含合理的 TUI 主题、上下文阈值（trigger_ratio=0.8、keep_recent_turns=3）等基础字段。

#### Scenario: 空配置场景

- **WHEN** 无任何配置文件，调用 `config.Load()`
- **THEN** 返回的 `Config.Context.TriggerRatio` 等于 0.8，`Config.Context.KeepRecentTurns` 等于 3，`Config.TUI.Theme` 非空字符串

### Requirement: 暴露已加载的文件列表

`config.Load()` 的返回值 MUST 包含本次加载真正读到的文件路径数组（按合并顺序）。文件不存在时 MUST NOT 出现在列表中。

#### Scenario: 全部存在

- **GIVEN** `~/.config/ub/config.yaml` 与 `./.ub/config.yaml` 均存在
- **WHEN** 调用 `config.Load()`
- **THEN** 返回的文件列表为 `["~/.config/ub/config.yaml", "./.ub/config.yaml"]`（按合并顺序）

#### Scenario: 仅本地存在

- **GIVEN** `~/.config/ub/config.yaml` 不存在，`./.ub/config.yaml` 存在
- **WHEN** 调用 `config.Load()`
- **THEN** 返回的文件列表仅包含 `./.ub/config.yaml`

### Requirement: `ub config show` 子命令

`ub config show` SHALL 把合并后的有效配置以 YAML 格式打印到 stdout，且 MUST 对带 `secret:"true"` tag 的字段（如 `api_key`）以 `***` 遮罩。

#### Scenario: 敏感字段被遮罩

- **GIVEN** 配置含 `providers.anthropic.api_key: sk-real-key`
- **WHEN** 用户运行 `ub config show`
- **THEN** stdout 含 `api_key: '***'`，不含 `sk-real-key`

#### Scenario: 空配置打印默认值

- **GIVEN** 无任何配置文件
- **WHEN** 用户运行 `ub config show`
- **THEN** stdout 为合法 YAML，包含 `context:` 节及默认值

### Requirement: `ub config path` 子命令

`ub config path` SHALL 把本次加载实际读到的文件路径按合并顺序逐行打印到 stdout。

#### Scenario: 列出已加载文件

- **GIVEN** 仅 `~/.config/ub/config.yaml` 存在
- **WHEN** 用户运行 `ub config path`
- **THEN** stdout 仅一行：`/home/<user>/.config/ub/config.yaml`

#### Scenario: 空时输出友好提示

- **GIVEN** 无任何配置文件
- **WHEN** 用户运行 `ub config path`
- **THEN** stdout 输出 `(no config files loaded; using built-in defaults)` 并以 exit code 0 退出

### Requirement: JSON Schema 生成

仓库 MUST 提供 `make schema` target，运行后生成 / 更新 `schema/config.schema.json`，内容由 `Config` Go 结构体反射得到。schema 文件 MUST 提交到 git 仓库。

#### Scenario: 生成 schema 文件

- **WHEN** 用户在仓库根运行 `make schema`
- **THEN** `schema/config.schema.json` 被创建或更新，且是合法 JSON Schema（含 `$schema`、`properties`）

#### Scenario: schema 覆盖关键字段

- **WHEN** 检查生成的 `schema/config.schema.json`
- **THEN** schema `properties` 至少包含 `default_model`、`small_model`、`providers`、`tui`、`context`、`permissions`、`mcp_servers`、`lsp_servers`

### Requirement: 不支持本迭代之外的能力

系统 MUST NOT 在本迭代内实现 `profiles:` 配置节、`/config reload` 热加载、配置写回、JSON 配置语言支持。这些被显式推迟到 I-13（profiles）或 V2（其余）。

#### Scenario: profiles 节存在时不报错也不生效

- **WHEN** 用户在 YAML 中放 `profiles: {dev: {default_model: x}}`
- **THEN** `config.Load()` 不报错（未知顶层键容忍），但 `Config` 上无 profile 行为

#### Scenario: 不接受 JSON 文件

- **WHEN** 用户把配置写在 `~/.config/ub/config.json`
- **THEN** 系统 MUST NOT 读取该文件；`config.Load()` 视为该层缺失
