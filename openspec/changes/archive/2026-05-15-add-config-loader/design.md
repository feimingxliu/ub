## Context

I-01 已落地 cobra 骨架（`cmd/ub`、`internal/cli`），其中 `ub config show` 与 `ub config path` 仍是返回 `not implemented (I-02)` 的占位。

后续所有迭代都需要从配置层获取参数：I-03 SQLite 路径、I-07 fake provider 注册、I-08 Anthropic 的 `api_key`/`base_url`/`headers`、I-09 rollout 写入路径、I-22 TUI 主题等。**先确立稳定的配置加载契约**，避免每个迭代独立去读环境变量或硬编码默认值。

约束：
- I-02 的 Out of Scope：JSON 配置语言、`profiles:` 节（I-13）、`/config reload`（V2）、配置写回（V2）
- 业务字段（providers / mcp_servers / lsp_servers 等）的*结构*要落到位（后续迭代填用），但*行为*不实现（本迭代不调用任何 provider、不启动任何 server）
- 与 `docs/design.md` §8 的 YAML 样例保持兼容

## Goals / Non-Goals

**Goals:**

- 一个 `config.Load()` 入口，返回合并后的 `Config` 加 *实际加载到的文件列表*
- 严格的分层顺序与合并语义：内置默认 → 全局 → 本地 → 环境变量
- 配置值里 `${ENV_VAR}` / `${ENV_VAR:-fallback}` 占位替换
- 编辑器友好的 JSON Schema（`schema/config.schema.json`），由 `invopop/jsonschema` 从 Go 结构体自动生成
- `ub config show` 输出可读 YAML、敏感字段遮罩
- `ub config path` 列出本次 *真正读到* 的配置文件，不存在的不列
- 高覆盖率单测，所有路径（合并 / env / 错误 / 默认值）都跑得到

**Non-Goals:**

- 不支持 JSON / TOML / INI 配置文件
- 不实现热加载、不支持运行时写回
- 不实现 `profiles:` 节（结构里也不预留字段，留待 I-13 自然 ADDED）
- 不做配置 schema 与文件内容的双向校验在每次 Load 都跑（schema 只供 IDE，避免增加启动延迟；I-02 不实现 schema-driven validation）
- 不连接 provider / MCP / LSP，仅把它们的结构体定义出来

## Decisions

### 1. YAML 库选 `goccy/go-yaml` 而非 `gopkg.in/yaml.v3`

**为什么**：`goccy/go-yaml` 支持 `${ENV_VAR}` 内置占位符替换（虽然我们仍打算自己实现一层以拿到更细粒度的报错），错误行号信息更友好，且与 Go 1.26 兼容性好。`gopkg.in/yaml.v3` 维护活跃度下降。

**代价**：依赖体积略大；学习一套不同的 marshaler API。

### 2. 合并策略：右覆盖左的"深合并"

各层用 `[]*Config` 表示，按优先级从低到高合入。基本规则：

- 对标量字段：右侧非零值覆盖左侧
- 对 map 字段（如 `providers`）：按 key 合并，同 key 时整个 value 替换（不做 value 内的子字段合并），避免出现"半个 provider 配置"
- 对 slice 字段：右侧整体替换左侧，**不**做拼接

理由：`providers.openai = {api_key: A, base_url: B}` 与 `providers.openai = {base_url: C}` 合并时如果做子字段合并，会得到一个 api_key 来自全局、base_url 来自本地的奇怪组合，调试起来困难。整 value 替换是最少惊讶的策略。

`docs/design.md` §8 的加载顺序明确写了"工作目录 → 环境变量"在最后，本设计严格遵守。

### 3. 环境变量替换的占位符语法

支持两种：

- `${VAR}` —— 替换为环境变量值；不存在时替换为空字符串，并在加载日志里 WARN（避免静默吞错）
- `${VAR:-default}` —— 不存在或为空时使用 default

实现：在 YAML 解析*之前*对原始字节流做 regex 替换（`\$\{[A-Z_][A-Z0-9_]*(:-[^}]*)?\}`）。**不**支持 `${VAR:?error}` 这种 bash 高级语法（V1 不需要）。

替换发生在解析前是为了让占位符能出现在 YAML 任意位置，包括 key（虽然不常用）。

### 4. 敏感字段遮罩

在 `Config` 结构体上加 tag `secret:"true"`：

```go
type ProviderConfig struct {
    APIKey  string `yaml:"api_key" secret:"true"`
    BaseURL string `yaml:"base_url"`
}
```

`ub config show` 调一个 `Redact(cfg)` 把所有 `secret:"true"` 字段替换为 `***`（保留长度信息留 V2）。`Redact` 通过反射递归遍历。

`config path` 不涉及配置内容，无需遮罩。

### 5. 文件查找路径

固定两个位置，按优先级低到高：

1. **全局**：`$XDG_CONFIG_HOME/ub/config.yaml`，回退 `~/.config/ub/config.yaml`
2. **本地**：从当前工作目录起向上 *最多 5 层*，找到的第一个 `.ub/config.yaml`

向上查找的目的：允许在子目录里调用 `ub`，仍然能加载到项目根的 `.ub/`。Crush、Claude Code 都用这种"向上找"模式。

### 6. JSON Schema 生成

不放在运行路径——做成 `cmd/gen-schema/`（main package + 小脚本），由 `make schema` 触发。生成器调 `invopop/jsonschema.Reflect(&config.Config{})`，序列化到 `schema/config.schema.json`。

为什么不在运行时生成：schema 内容稳定、对开发体验是"提示"而非"校验"。把生成移到构建时可让二进制保持小，且让 schema 文件进入 git diff 视野（IDE 直接 follow 仓库版本）。

CI 可加 drift 检查（`make schema && git diff --exit-code schema/`），V2 再做。

### 7. CLI 接入

`internal/cli/root.go` 里 `newConfigCmd()` 的 `show` / `path` 子命令，原来的 `notImplemented("I-02")` 替换为：

```go
RunE: func(cmd *cobra.Command, _ []string) error {
    cfg, files, err := config.Load()
    if err != nil { return err }
    return config.WriteRedactedYAML(cmd.OutOrStdout(), cfg)
}
```

`Load()` 失败时（例如非法 YAML）直接把 error 透传给 cobra；I-04 引入 slog 后再加 structured logging。

## Risks / Trade-offs

- **风险**：用反射做 `Redact` 性能不佳，但 `config show` 是手工调用、非热路径，可接受
- **风险**：`${VAR}` 占位符在 base64 值或包含 `$` 的字面字符串里会被误替换 → 缓解：在文档里说明 `$$` 转义；实现里识别 `$$` 不替换
- **风险**：deep merge 策略与用户预期不符，例如希望 `providers.openai.api_key` 来自全局、`base_url` 来自本地 → 缓解：用户可以在本地配置里把 api_key 显式重复一次。如果反馈很多再升级到深合并（V2）
- **风险**：goccy/go-yaml 与 invopop/jsonschema 用的是不同的 struct tag（`yaml:` vs `json:`） → 缓解：两个 tag 都打，单测覆盖一致性
- **风险**：JSON Schema drift（结构体改了但忘记跑 `make schema`） → 缓解：本迭代不做强检查，V2 加 CI gate
