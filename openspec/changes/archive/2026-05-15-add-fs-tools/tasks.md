## 1. 依赖与包骨架

- [x] 1.1 `go get github.com/bmatcuk/doublestar/v4` 与 `github.com/aymanbagabas/go-udiff` 后 `go mod tidy`
- [x] 1.2 新建 `internal/tool/fs/` 目录，加包级注释说明沙箱约束与 PreviewableTool 实现位置
- [x] 1.3 新建 `internal/tool/fs/path.go`，实现 `resolve(root, path) (string, error)` 路径校验

## 2. 工具实现

- [x] 2.1 `read.go`：实现 `read` 工具（带行号、offset/limit、2000 行截断）
- [x] 2.2 `ls.go`：实现 `ls` 工具（按名字字典序，kind\tname 输出）
- [x] 2.3 `glob.go`：实现 `glob` 工具（doublestar 递归匹配，返回相对 root 的字典序结果）
- [x] 2.4 `write.go`：实现 `write` 工具，Execute 覆盖写、Preview 返回 create/modify unified diff
- [x] 2.5 `edit.go`：实现 `edit` 工具，Preview/Execute 共享匹配校验，Execute 阶段再校验现盘一致性

## 3. Register 入口

- [x] 3.1 `register.go`：实现 `fs.Register(reg *tool.Registry, root string) error`，依次注册五个工具
- [x] 3.2 注册前 `filepath.Clean(root)`，把 root 显式注入到每个工具实例

## 4. 单测

- [x] 4.1 `path_test.go`：覆盖相对 / 绝对 / `..` 跳出 / root 内绝对路径
- [x] 4.2 `read_test.go`：覆盖整文件、offset/limit、超大文件截断
- [x] 4.3 `ls_test.go`：覆盖正常列出、按字典序、对文件 ls 报错
- [x] 4.4 `glob_test.go`：覆盖递归 `**` 匹配、空结果、排序
- [x] 4.5 `write_test.go`：覆盖 create / modify Preview 不改盘、Execute 写盘并填 Result.Files
- [x] 4.6 `edit_test.go`：覆盖单匹配、多匹配 replace_all、多匹配未开报错、old 不存在报错、Execute 时磁盘变更检测
- [x] 4.7 `register_test.go`：覆盖 `fs.Register` 把五个工具注册成功 + 与已存在工具同名时返回错误

## 5. 验证

- [x] 5.1 运行 `go test ./internal/tool/fs/...`
- [x] 5.2 运行 `go test ./...`
- [x] 5.3 运行 `make lint`
- [x] 5.4 运行 `make build`
- [x] 5.5 运行 `openspec validate add-fs-tools --strict`
