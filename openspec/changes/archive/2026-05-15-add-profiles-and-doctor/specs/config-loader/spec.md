## MODIFIED Requirements

### Requirement: 分层合并

系统 SHALL 按"内置默认 → 全局（`~/.config/ub/config.yaml`）→ 本地（当前工作目录向上最多 5 层找到的第一个 `.ub/config.yaml`）→ profile 覆盖 → CLI mode 覆盖"的顺序加载并合并配置。后加载层 MUST 整字段覆盖（map 按 key 替换 value、slice 整体替换、标量按非零值覆盖）。系统 MUST 容忍任一配置文件层缺失，不视为错误。

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

#### Scenario: profile 覆盖本地配置

- **WHEN** 本地配置含 `default_model: fake/base`，且应用的 profile 含 `default_model: fake/dev`
- **THEN** 最终 `Config.DefaultModel` 等于 `fake/dev`

### Requirement: JSON Schema 生成

仓库 MUST 提供 `make schema` target，运行后生成 / 更新 `schema/config.schema.json`，内容由 `Config` Go 结构体反射得到。schema 文件 MUST 提交到 git 仓库。

#### Scenario: 生成 schema 文件

- **WHEN** 用户在仓库根运行 `make schema`
- **THEN** `schema/config.schema.json` 被创建或更新，且是合法 JSON Schema（含 `$schema`、`properties`）

#### Scenario: schema 覆盖关键字段

- **WHEN** 检查生成的 `schema/config.schema.json`
- **THEN** schema `properties` 至少包含 `default_model`、`small_model`、`execution_mode`、`approval_agent`、`profiles`、`providers`、`tui`、`context`、`permissions`、`mcp_servers`、`lsp_servers`

### Requirement: 不支持本迭代之外的能力

系统 MUST NOT 在本迭代内实现 `/config reload` 热加载、配置写回、JSON 配置语言支持。这些能力被显式推迟到 V2。系统 SHALL 在本迭代内正式支持 `profiles:` 配置节。

#### Scenario: profiles 节生效

- **WHEN** 用户在 YAML 中放 `profiles: {dev: {default_model: fake/dev}}` 并选择 `dev` profile
- **THEN** `config.LoadWithOptions()` MUST 应用该 profile 并更新有效配置

#### Scenario: 不接受 JSON 文件

- **WHEN** 用户把配置写在 `~/.config/ub/config.json`
- **THEN** 系统 MUST NOT 读取该文件；`config.Load()` 视为该层缺失
