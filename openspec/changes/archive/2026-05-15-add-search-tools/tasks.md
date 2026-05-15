## 0. 共享 path helper 提升

- [x] 0.1 在 `internal/tool/path.go` 新增导出函数 `Resolve(root, path string) (string, error)`，逻辑等同于 `add-fs-tools` 中的 `resolve`
- [x] 0.2 把 `internal/tool/fs/path.go` 改为薄封装直接调用 `tool.Resolve`（保留 fs 包内的 private alias 以减少调用面改动）
- [x] 0.3 重跑 `go test ./internal/tool/fs/...`，确认沙箱测试通过

## 1. 包骨架与依赖

- [x] 1.1 新建 `internal/tool/search/` 目录，加包级注释说明双后端策略与默认实现
- [x] 1.2 在 `backend.go` 定义 `backend` 接口、`grepOpts`、`grepHit` 类型
- [x] 1.3 暴露包级注入点 `var newBackend = defaultNewBackend` 以便测试覆盖

## 2. 内置 Go 后端

- [x] 2.1 `go_backend.go`：实现 `goBackend`，用 `filepath.WalkDir` + `regexp` + `bufio.Scanner`
- [x] 2.2 二进制检测：首 8 KB 含 `\x00` 即跳过
- [x] 2.3 `include` 过滤：用 `doublestar.PathMatch(include, relPath)` 过滤候选路径
- [x] 2.4 路径相对化：`filepath.Rel(root, abs)` 后把 `\` 替换为 `/`
- [x] 2.5 单行长度超 2048 字节时截断并附加 ` ...(truncated)`

## 3. ripgrep 后端

- [x] 3.1 `rg_backend.go`：实现 `rgBackend`，通过 `os/exec` 构造 `rg --line-number --no-heading --color=never --no-messages [-g include] PATTERN PATH`
- [x] 3.2 解析 rg 输出为 `[]grepHit`；遇到非 `path:line:rest` 形式的行视为错误
- [x] 3.3 rg 后端先实现但默认不启用，仅通过注入点接入

## 4. grep 工具实现

- [x] 4.1 `grep.go`：实现 `Tool` 接口，`Name="grep"`、`Risk=tool.RiskSafe`
- [x] 4.2 input 结构体含 `pattern`、`path`、`include` 字段，`Schema()` 通过 `invopop/jsonschema` 生成
- [x] 4.3 Execute：parse args → `regexp.Compile` → `tool.Resolve` 校验 path → 调 `newBackend(root).run(ctx, ...)` → 排序 → 拼接 `Result.Content`
- [x] 4.4 排序：先按 `Path` 升序、再按 `Line` 升序，保证 deterministic
- [x] 4.5 truncation 与无匹配行为按 spec 实现

## 5. Register 入口

- [x] 5.1 `register.go`：实现 `search.Register(reg *tool.Registry, root string) error`
- [x] 5.2 注册前 `filepath.Clean(root)`；与已有同名工具冲突时返回 Registry 的原始错误

## 6. 单测

- [x] 6.1 `go_backend_test.go`：覆盖 happy path、`include` 过滤、二进制文件跳过、单行截断
- [x] 6.2 `rg_backend_test.go`：用注入的 fake `commandRunner` 接口断言 rg 调用 args 与解析输出（不依赖系统 rg）
- [x] 6.3 `grep_test.go`：通过注入 fake backend 覆盖 schema、Execute、排序、`Resolve` 沙箱跳出报错
- [x] 6.4 `grep_test.go`：覆盖非法正则、缺省 path、无匹配返回空 Content
- [x] 6.5 `register_test.go`：覆盖 `search.Register` 成功与冲突报错；并断言默认 backend 是 `goBackend`

## 7. 验证

- [x] 7.1 运行 `go test ./internal/tool/...`
- [x] 7.2 运行 `go test ./...`
- [x] 7.3 运行 `make lint`
- [x] 7.4 运行 `make build`
- [x] 7.5 运行 `openspec validate add-search-tools --strict`
