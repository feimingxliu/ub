# ub 安装指南

本文档覆盖 ub 的源码构建、`go install` 安装、Release 二进制安装、本地配置、平台差异、升级与卸载。

英文 README 见 [`../README.md`](../README.md)。中文主文档见 [`../README.zh-CN.md`](../README.zh-CN.md)，使用细节见 [`usage.md`](usage.md)。

## 1. 前置依赖

**必须**：

- Go 工具链：版本不低于 `go.mod` 中的最低要求（当前 Go 1.25+）。
  - 检查：`go version`
  - 安装：[https://go.dev/dl/](https://go.dev/dl/)

**可选（推荐安装）**：

| 工具 | 用途 | 缺失时影响 |
|---|---|---|
| `rg`（ripgrep） | 加速 `grep` 工具 | 自动回退到 Go 内置实现，速度稍慢 |
| `gopls` | Go LSP 服务 | LSP 工具（`diagnostics` / `references` / `hover` / `completion` / `document_symbols` / `rename` / `code_action`）对 Go 项目不可用 |
| `typescript-language-server` | TypeScript / JavaScript LSP | LSP 工具对 TS/JS 项目不可用 |
| `npx`（Node.js） | 运行某些基于 npm 的 MCP server | 无法用 `npx -y @some/mcp-server` 方式启动 MCP |
| `gofumpt` | 代码格式化（开发用） | `make fmt` 回退到 `gofmt` |

**Provider 凭证**：

- 想用 Anthropic：`ANTHROPIC_API_KEY`
- 想用 OpenAI：`OPENAI_API_KEY`
- 想用 OpenAI 兼容端点（DeepSeek / Together / vLLM / Ollama `/v1` 等）：对应服务的 API Key 与 base_url
- 都不想用：可以用 `fake` provider 跑通流程（无需任何 key）

## 2. 安装方式

> **推荐：直接下载 Release 二进制。** Go 的交叉编译让我们能为每个平台提供独立的开箱即用二进制，无需本地装 Go 工具链。源码 / `go install` 方式适合开发者或需要 unreleased 特性的场景。

### 2.1 从 Release 归档安装（推荐）

发布版本由 [GoReleaser](https://goreleaser.com/) 构建，覆盖 Linux / macOS / Windows × amd64 / arm64。每次发布包含 `.tar.gz`（Unix）/ `.zip`（Windows）+ `checksums.txt`。
Release workflow 同时生成 Syft SBOM，并用 Cosign keyless 为归档和 `checksums.txt` 生成 `.sig` / `.pem`，需要时可以按下方可选步骤校验。

**Linux / macOS**：

```sh
# 自动取最新版本（替换 PLATFORM 为目标平台，例如 linux_amd64 / darwin_arm64 / linux_arm64）
PLATFORM=linux_amd64
curl -LO "https://github.com/feimingxliu/ub/releases/latest/download/ub_${PLATFORM}.tar.gz"
tar -xzf "ub_${PLATFORM}.tar.gz"
install -m 0755 ub ~/.local/bin/ub
ub --version
```

也可以从 [Releases 页面](https://github.com/feimingxliu/ub/releases) 手动下载对应版本。

**macOS** 上首次运行如果被 Gatekeeper 拦截：

```sh
xattr -d com.apple.quarantine $(which ub)
```

或在「系统设置 → 隐私与安全性」点击「仍要打开」。

**Windows（PowerShell）**：

```powershell
# 下载最新版本
Invoke-WebRequest -Uri https://github.com/feimingxliu/ub/releases/latest/download/ub_windows_amd64.zip -OutFile ub.zip
Expand-Archive .\ub.zip -DestinationPath .

# 放到可执行路径
Move-Item .\ub.exe $Env:LOCALAPPDATA\Microsoft\WindowsApps\ub.exe
ub --version
```

#### 可选：校验签名和 checksum

Release 资产会附带 `checksums.txt`、每个归档的 `.sig` / `.pem`、以及 SBOM。需要更严格校验时再执行这一段：

```sh
PLATFORM=linux_amd64
curl -LO "https://github.com/feimingxliu/ub/releases/latest/download/ub_${PLATFORM}.tar.gz.sig"
curl -LO "https://github.com/feimingxliu/ub/releases/latest/download/ub_${PLATFORM}.tar.gz.pem"
curl -LO https://github.com/feimingxliu/ub/releases/latest/download/checksums.txt
curl -LO https://github.com/feimingxliu/ub/releases/latest/download/checksums.txt.sig
curl -LO https://github.com/feimingxliu/ub/releases/latest/download/checksums.txt.pem

cosign verify-blob \
  --certificate "ub_${PLATFORM}.tar.gz.pem" \
  --signature "ub_${PLATFORM}.tar.gz.sig" \
  --certificate-identity-regexp 'https://github.com/feimingxliu/ub/.github/workflows/release.yaml@refs/tags/v.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  "ub_${PLATFORM}.tar.gz"
cosign verify-blob \
  --certificate checksums.txt.pem \
  --signature checksums.txt.sig \
  --certificate-identity-regexp 'https://github.com/feimingxliu/ub/.github/workflows/release.yaml@refs/tags/v.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
sha256sum -c --ignore-missing checksums.txt
```

### 2.2 通过 `go install`

如果你已经有 Go 工具链，并希望跟主干或指定 tag：

```sh
go install github.com/feimingxliu/ub/cmd/ub@latest
go install github.com/feimingxliu/ub/cmd/ub@v0.1.0     # 指定版本
```

二进制会被装到 `$(go env GOBIN)`，默认 `~/go/bin/ub`。确认该目录在 `PATH` 中（见下文 §3）。

### 2.3 从源码构建

需要 hack 内部代码或在 release 之前抢先试时使用：

```sh
git clone https://github.com/feimingxliu/ub.git
cd ub
go build -o ub ./cmd/ub
./ub --version
```

或用 Makefile：

```sh
make build      # 构建二进制到 ./ub
make test       # 运行所有测试
make vet
make lint       # vet + 格式检查
make fmt        # 使用 gofumpt 格式化
make schema     # 重新生成 api/config.schema.json
```

把构建产物放到 PATH：

```sh
install -m 0755 ./ub ~/.local/bin/ub
```

如果默认 Go 构建缓存不可写（受限沙箱）：

```sh
GOCACHE=/tmp/ub-go-build go build -o ub ./cmd/ub
GOCACHE=/tmp/ub-go-build go test ./...
```

## 3. 把 ub 加入 PATH

如果 `ub --version` 提示找不到命令，需要把安装目录加进 `PATH`。

**Linux / macOS（bash / zsh）**：

```sh
# 如果用 go install
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.bashrc   # 或 ~/.zshrc
# 如果安装到 ~/.local/bin
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc
exec $SHELL -l
```

**Windows**：检查 `Path` 环境变量包含 `%LOCALAPPDATA%\Microsoft\WindowsApps`，或者把 `ub.exe` 放到任意已在 `Path` 中的目录。

> macOS Gatekeeper 的处理已经写在 [§2.1](#21-从-release-归档安装推荐)。

> Windows 说明：仓库的 Platform workflow 会在 `windows-latest` 上运行
> `make build`、`make test`、`make vet`，并用 zip 归档做一次
> `ub.exe --version` / `ub.exe run --help` 安装烟测。WSL 验证不属于当前自动化门禁。

## 4. 配置 Provider

ub 启动时按以下顺序合并配置：

1. 内置默认值
2. `~/.config/ub/config.yaml`（全局）
3. `./.ub/config.yaml`（当前工作区，覆盖全局）
4. 环境变量替换（`${ENV_VAR}` 形式）
5. Profile 选择（`--profile`、`--dev` 或 `UB_PROFILE`）
6. CLI 运行时参数（`--mode`、`--provider`、`--model`）

### 4.1 OpenAI

```yaml
default_provider: openai
default_model: gpt-4o-mini
providers:
  openai:
    type: openai
    api_key: ${OPENAI_API_KEY}
```

```sh
export OPENAI_API_KEY=sk-...
ub chat "say PONG"
```

### 4.2 Anthropic

```yaml
default_provider: anthropic
default_model: claude-sonnet-4-20250514
providers:
  anthropic:
    type: anthropic
    api_key: ${ANTHROPIC_API_KEY}
```

```sh
export ANTHROPIC_API_KEY=sk-ant-...
ub chat "say PONG"
```

### 4.3 OpenAI 兼容端点（DeepSeek / Together / vLLM / LiteLLM / Cloudflare AI Gateway / Azure 等）

```yaml
default_provider: deepseek
default_model: deepseek-chat
providers:
  deepseek:
    type: openai-compat
    api_key: ${DEEPSEEK_API_KEY}
    base_url: https://api.deepseek.com/v1
    timeout: 5m
```

本地 vLLM 示例：

```yaml
default_provider: local
default_model: openai/gpt-oss-20b
providers:
  local:
    type: openai-compat
    base_url: http://127.0.0.1:8000/v1
    timeout: 10m
```

### 4.4 Ollama（走 OpenAI 兼容端点）

Ollama 内置 `/v1` OpenAI 兼容协议，直接用 `openai-compat` 即可：

```yaml
default_provider: ollama
default_model: qwen3
providers:
  ollama:
    type: openai-compat
    base_url: http://localhost:11434/v1
    api_key: ollama   # 任意非空字符串
```

确认本机已运行：

```sh
ollama serve &     # 如果未启动
ollama pull qwen3
ub chat "hello"
```

### 4.5 Fake Provider（离线测试）

```yaml
default_provider: fake
default_model: fake/demo
providers:
  fake:
    type: fake
    script:
      - type: text_delta
        text: hello from ub
      - type: done
```

适合在没有 API key 的情况下打通 TUI / 工具调用 / rollout 链路，所有 CI 测试也用它。

### 4.6 多 Provider 同时配置

把多个 provider 同时配置，运行时通过 `--provider` 或 TUI 内 `/provider` 命令切换：

```yaml
default_provider: openai
default_model: gpt-4o-mini

providers:
  openai:
    type: openai
    api_key: ${OPENAI_API_KEY}
  anthropic:
    type: anthropic
    api_key: ${ANTHROPIC_API_KEY}
  deepseek:
    type: openai-compat
    api_key: ${DEEPSEEK_API_KEY}
    base_url: https://api.deepseek.com/v1
  local:
    type: openai-compat
    base_url: http://127.0.0.1:8000/v1
  ollama:
    type: openai-compat
    base_url: http://localhost:11434/v1
    api_key: ollama
```

### 4.7 后台 Job 工具

`job_run` / `job_output` / `job_kill` 的生命周期可以通过 `tools.job` 调整：

```yaml
tools:
  job:
    max_concurrent: 50
    retention: 8h
    cleanup_interval: 5m
```

- `max_concurrent`：manager 内同时保留的 job 上限，达到后 `job_run` 会拒绝新任务。
- `retention`：已完成 job 在 manager 内保留多久，超时后由后台清扫器移除。
- `cleanup_interval`：长跑进程中的后台清扫周期。

## 5. 验证安装

```sh
ub --version          # 版本号 + go 构建信息
ub config show        # 合并后的有效配置
ub config path        # 列出加载的配置文件
ub doctor --plain     # 健康检查：探测各 provider base_url、检查可选依赖
```

`ub doctor` 会逐项报告：

- Provider base_url 连通性（OpenAI / Anthropic / 兼容端点）
- 可用模型列表（含是否声明支持 tool calling）
- 系统依赖：`rg` / `gopls` / `typescript-language-server` / `npx` 是否可执行
- 配置文件加载链

第一次跑：

```sh
ub chat "hello"       # 单轮直接对话，最快验证 provider 通畅
ub run -p "ls"        # 无头 agent + 工具调用，验证工具链
ub                    # 进入 TUI
```

## 6. 存储与数据目录

| 数据 | 默认路径 | 控制变量 |
|---|---|---|
| 会话数据库（SQLite） | `$XDG_DATA_HOME/ub/ub.db`，回退 `~/.local/share/ub/ub.db` | `XDG_DATA_HOME` |
| 工具结果溢出文件 | `$XDG_STATE_HOME/ub/`，回退 `~/.local/state/ub/` | `XDG_STATE_HOME` |
| TUI 日志 | `$XDG_STATE_HOME/ub/ub.log` | `UB_LOG_FILE` 可覆盖 |
| 项目权限规则 | `<workspace>/.ub/permissions.yaml` | 由 ub 自动管理，保存 Claude-style allow/ask/deny command rules |

启动期自动清理（best-effort）：

```yaml
cleanup:
  enabled: true
  interval: 24h
  sessions:
    max_age: 720h                  # 30 天
    min_recent_per_workspace: 20   # 每个工作区至少保留 20 个最近 session
  logs:
    max_size_mb: 10
    max_backups: 5
```

## 7. 升级

**Release 归档（推荐）**：重新走 [§2.1](#21-从-release-归档安装推荐) 流程，下载新版本覆盖旧二进制即可。

**`go install` 方式**：

```sh
go install github.com/feimingxliu/ub/cmd/ub@latest
```

**从源码构建**：

```sh
cd <ub 仓库目录>
git pull
make build
install -m 0755 ./ub ~/.local/bin/ub
```

升级前可以备份会话数据：

```sh
cp ~/.local/share/ub/ub.db ~/ub.db.bak
```

## 8. 卸载

```sh
# 删除二进制（按你的安装路径调整）
rm -f ~/go/bin/ub
rm -f ~/.local/bin/ub
rm -f $LOCALAPPDATA/Microsoft/WindowsApps/ub.exe   # Windows

# 删除配置（可选）
rm -f ~/.config/ub/config.yaml
rm -f .ub/permissions.yaml

# 删除会话与状态数据（可选）
rm -rf ~/.local/share/ub
rm -rf ~/.local/state/ub
```

## 9. 环境变量速查

| 变量 | 作用 |
|---|---|
| `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / `DEEPSEEK_API_KEY` 等 | 通过 `${VAR}` 注入到 provider 配置 |
| `UB_PROFILE` | 默认使用的 profile 名 |
| `UB_LOG_LEVEL` | `debug` / `info`（默认）/ `warn` / `error` |
| `UB_LOG_FILE` | 自定义日志文件路径（JSON 格式） |
| `UB_VCR` | `record` / `replay` / `disabled`，仅测试场景使用 |
| `XDG_DATA_HOME` / `XDG_STATE_HOME` / `XDG_CONFIG_HOME` | 控制数据 / 状态 / 配置目录位置 |
| `GOCACHE` | 受限沙箱中重定向 Go 构建缓存 |

## 10. 常见安装问题

- **`go install` 提示 `module not found`**：仓库尚未公开发布或 module path 写错。临时改用 §2.2 源码构建。
- **`ub --version` 提示 command not found**：PATH 未配置，见 §3。
- **macOS 提示「无法验证开发者」**：见 §2.1 末尾的 Gatekeeper 处理。
- **`ub doctor` 标 `rg ✗`**：可选依赖，不影响主流程；想用就 `apt install ripgrep` / `brew install ripgrep`。
- **`ub doctor` 报 base_url 不可达**：`curl <base_url>/models`（OpenAI 兼容）确认能响应；用本地 Ollama 时确认 `ollama serve` 已启动并能访问 `http://localhost:11434/v1/models`；网络代理是否生效。
- **TUI 显示乱码 / 字符错位**：终端不支持 UTF-8 或字体不全；或者你触发了已知 bug 之前的版本（参考最新 release notes）。

更多故障排查与日常使用见 [`usage.md`](usage.md)。
