## Why

I-01 已经把 cobra 骨架搭好，但所有占位子命令都还没有可用的配置入口。后续所有迭代（provider 接入、SQLite 路径、TUI 主题、上下文阈值等）都需要从一个集中、可分层、可校验的配置入口读取参数。先把配置加载层落实，避免每个迭代各自硬编码或临时读环境变量。

## What Changes

- 新增 `internal/config/` 包，定义 `Config` 结构体（覆盖 design §8 的字段：`providers`、`default_model`、`small_model`、`tui`、`permissions`、`mcp_servers`、`lsp_servers`、`context`，**不含** `profiles`，留待 I-13）
- 实现 `config.Load()`：按"内置默认 → `~/.config/ub/config.yaml` → 工作目录 `.ub/config.yaml` → 环境变量覆盖"顺序加载并合并；支持配置值里的 `${ENV_VAR}` 占位符替换；返回最终 `Config` 加一份 *实际加载到的文件列表*
- 选用 `goccy/go-yaml` 作为 YAML 解析库；不支持 JSON 作为配置语言（JSON 只用于 schema 校验）
- 用 `invopop/jsonschema` 在构建时生成 `schema/config.schema.json`，供编辑器自动补全 / 校验
- 把 I-01 的占位子命令 `ub config show` / `ub config path` 接到真实实现：
  - `show`：打印合并后的有效配置（YAML 格式，敏感字段如 `api_key` 自动遮罩为 `***`）
  - `path`：列出本次实际读取的配置文件路径
- 在 `Makefile` 加 `schema` target：跑生成器，重新写出 `schema/config.schema.json`
- 占位语义保持：`provider` 等结构体先按 design §5 的 `ProviderConfig` 形状落地，但 *不* 在本迭代里调用任何 provider；I-07 才会把 `Config.Providers` 喂给工厂

## Capabilities

### New Capabilities

- `config-loader`: 提供分层 YAML 配置的加载、合并、环境变量替换、敏感字段遮罩与 JSON Schema 生成。是后续所有需要"读用户配置"的模块的统一入口。

### Modified Capabilities

（无 —— 这是首个 capability）

## Impact

- **新增代码**：`internal/config/`（含 `Config` 结构体、`Load()`、`Merge()`、env 替换、文件查找）
- **修改代码**：`internal/cli/root.go`（把 `config show` / `config path` 的占位 `notImplemented` 替换为真实 `RunE`）
- **新增生成物**：`schema/config.schema.json`（提交到仓库；CI 可加 drift 检查留 V2）
- **新增依赖**：
  - `github.com/goccy/go-yaml`（YAML 解析）
  - `github.com/invopop/jsonschema`（schema 生成）
- **不引入**：JSON 配置语言、配置文件热加载、配置写回、`profiles:` 节（均按 docs/requirements.md §6 与 docs/roadmap.md I-02 的 Out of Scope 明确推迟）
- **测试**：`internal/config/` 全套单测（env 替换 / 分层覆盖 / 缺字段默认值 / 非法 YAML / 敏感字段遮罩）
