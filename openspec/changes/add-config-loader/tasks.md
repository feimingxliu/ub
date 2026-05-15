## 1. 依赖与目录骨架

- [x] 1.1 在 `go.mod` 引入 `github.com/goccy/go-yaml` 与 `github.com/invopop/jsonschema`（用 `GOPROXY=https://goproxy.cn,direct go get`）
- [x] 1.2 创建 `internal/config/` 目录
- [x] 1.3 创建 `cmd/gen-schema/` 目录（独立 main package，仅供 `make schema` 调用）
- [x] 1.4 在 `schema/` 目录建占位 `.gitkeep`（首次 `make schema` 后会生成 `config.schema.json`）

## 2. Config 结构体定义

- [x] 2.1 在 `internal/config/types.go` 定义顶层 `Config` 结构体（字段：`DefaultModel`、`SmallModel`、`Providers`、`TUI`、`Permissions`、`MCPServers`、`LSPServers`、`Context`）
- [x] 2.2 定义 `ProviderConfig`（含 `Type`、`APIKey`（带 `secret:"true"` tag）、`BaseURL`、`Headers`、`Timeout`）
- [x] 2.3 定义 `TUIConfig`、`PermissionConfig`、`MCPServerConfig`、`LSPServerConfig`、`ContextConfig`
- [x] 2.4 每个字段同时打 `yaml:` 与 `json:` tag（一致性）；非必填字段加 `omitempty`
- [x] 2.5 顶层 `Config` 上加 `Unknown map[string]any \`yaml:",inline"\``（吞掉未知顶层键如 `profiles`，对应 spec "未知顶层键容忍"）

## 3. 加载与合并

- [x] 3.1 在 `internal/config/defaults.go` 实现 `func Defaults() *Config`，返回内置默认（context.TriggerRatio=0.8 等）
- [x] 3.2 在 `internal/config/locate.go` 实现 `globalConfigPath()`（XDG_CONFIG_HOME → ~/.config/ub/config.yaml）
- [x] 3.3 在 `internal/config/locate.go` 实现 `localConfigPath(cwd string)`：从 cwd 向上最多 5 层找 `.ub/config.yaml`
- [x] 3.4 在 `internal/config/merge.go` 实现 `Merge(layers ...*Config) *Config`，按 design §2 的规则：标量右覆盖左、map 按 key 替换 value、slice 整体替换
- [x] 3.5 在 `internal/config/load.go` 实现 `Load() (*Config, []string, error)`：定位文件 → 替换 env → YAML 解码 → Merge defaults+globalConfig+localConfig

## 4. 环境变量替换

- [x] 4.1 在 `internal/config/env.go` 实现 `Expand(b []byte) []byte`：用 regex `\$\$|\$\{[A-Z_][A-Z0-9_]*(:-[^}]*)?\}` 匹配；`$$` 输出 `$`；变量未设置 → 默认值或空字符串
- [x] 4.2 缺失变量时通过 `slog.Warn`（暂时也可临时用 stderr 日志，I-04 再统一）
- [x] 4.3 `Expand` 在 YAML 解析*之前*作用于原始字节流

## 5. 敏感字段遮罩

- [x] 5.1 在 `internal/config/redact.go` 实现 `Redact(cfg *Config) *Config`：通过反射深拷贝并把 `secret:"true"` 的 string 字段替换为 `***`
- [x] 5.2 处理嵌套：结构体、map[string]ProviderConfig 等递归走到底

## 6. CLI 接入

- [x] 6.1 在 `internal/cli/root.go` 把 `newConfigCmd()` 下 `show` 子命令的 `notImplemented("I-02")` 替换为：调 `config.Load()` → `config.Redact(cfg)` → 用 `goccy/go-yaml` marshal 到 `cmd.OutOrStdout()`
- [x] 6.2 把 `path` 子命令的 `notImplemented("I-02")` 替换为：调 `config.Load()` → 逐行打印加载到的文件列表；列表为空时打印 `(no config files loaded; using built-in defaults)`
- [x] 6.3 删除 `internal/cli/root_test.go` 里针对 I-02 的占位错误断言（`config show`、`sessions ls` 中前两条），改成断言 `config show` 在空配置下退出码 0 且 stdout 是合法 YAML

## 7. JSON Schema 生成器

- [x] 7.1 在 `cmd/gen-schema/main.go` 写一个 ~30 行的 main：`jsonschema.Reflect(&config.Config{})` → 写到 `schema/config.schema.json`（pretty print，2 空格缩进）
- [x] 7.2 在 `Makefile` 增加 `schema` target：`go run ./cmd/gen-schema`
- [x] 7.3 首次跑一次 `make schema`，把生成的 `schema/config.schema.json` 提交进 git

## 8. 测试

- [x] 8.1 `internal/config/env_test.go`：覆盖普通替换、默认值回退、`$$` 转义、缺失变量、变量名格式
- [x] 8.2 `internal/config/locate_test.go`：覆盖向上找 5 层（用 `t.TempDir()` + 多级子目录模拟）
- [x] 8.3 `internal/config/merge_test.go`：覆盖 design §2 的三种类型（标量、map、slice）合并；尤其 `providers.openai = {api_key: A, base_url: B}` 与 `{base_url: C}` 合并的 value-替换行为
- [x] 8.4 `internal/config/load_test.go`：构造临时 home + 临时 cwd，覆盖空配置 / 仅全局 / 仅本地 / 都有 / 非法 YAML（断言 error 含路径与近似行号）
- [x] 8.5 `internal/config/redact_test.go`：构造含 api_key 的 Config，断言 Redact 后 api_key 为 `***`，其他字段不变
- [x] 8.6 `internal/cli/config_test.go`：用 cobra `SetOut/SetArgs` 端到端测 `ub config show` 与 `ub config path`（构造临时 home，注入到 env，断言 stdout 内容）
- [x] 8.7 确保 `go test ./... -race -count=1` 全绿，单测覆盖率 `internal/config/` ≥ 80%

## 9. 收尾

- [x] 9.1 跑 `make lint`、`make test`、`make build`，全部通过
- [x] 9.2 手测：`./ub config show` 空配置下打印默认 YAML；放一个 `~/.config/ub/config.yaml` 含 `api_key` 后再跑，断言被遮罩
- [x] 9.3 手测：`./ub config path` 空时输出友好提示，存在文件时输出文件列表
- [x] 9.4 `git add` 仅业务相关文件（不带二进制 `ub`、不带 `schema/config.schema.json` 之外的生成物）
- [x] 9.5 提交：`[I-02] add YAML config loader with layered merge, env expansion, JSON Schema`
