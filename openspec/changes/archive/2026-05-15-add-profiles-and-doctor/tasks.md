## 1. 配置与 profile

- [x] 1.1 扩展 `Config` 类型：`execution_mode`、`approval_agent`、`profiles`、`tools_disabled`
- [x] 1.2 更新 merge/default/schema 相关逻辑
- [x] 1.3 实现 `LoadWithOptions`、`ApplyProfile`、`UB_PROFILE`、`--dev` 与 `--mode` 覆盖
- [x] 1.4 覆盖 profile 叠加、缺失 profile、非法 mode、CLI mode 覆盖测试

## 2. CLI flags 集成

- [x] 2.1 在 root command 增加全局 `--profile`、`--dev`、`--mode`
- [x] 2.2 让 `config show`、`config path` 和 `chat` 使用带 options 的配置加载
- [x] 2.3 覆盖 `ub config show --dev`、`--profile` 与冲突参数测试

## 3. doctor

- [x] 3.1 新增 `ub doctor` 子命令与 `--plain`、`--suggest` flags
- [x] 3.2 实现 OpenAI/openai-compat `/models` 与 Ollama `/api/tags` 探测
- [x] 3.3 实现外部命令 `rg`、`gopls`、`typescript-language-server`、`npx` 检查
- [x] 3.4 实现 dev profile 建议片段输出
- [x] 3.5 覆盖 mock provider endpoint、`--dev` profile awareness、NO_API_KEY 与 suggest 测试

## 4. 验证

- [x] 4.1 运行 `make schema`
- [x] 4.2 运行 `go test ./...`
- [x] 4.3 运行 `make lint`
- [x] 4.4 运行 `make build`
- [x] 4.5 运行 `openspec validate add-profiles-and-doctor --strict`
