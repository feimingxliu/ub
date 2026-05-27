## 1. 实现

- [x] 1.1 在 `internal/tool/fs/multiedit.go` 新增 `multiedit` 工具:
  - args struct:`{Edits []editArgs}`
  - Name/Description/Schema/Risk
  - `parseAndGroup`:解析 + 把 edits 按 path 分组,统一 resolve
  - `Preview`:依次读每个文件,内存中串行应用同 path 的所有 edits,产出每文件一条 FileDiff
  - `Execute`:同 Preview 的内存计算,然后对每文件二次 read 校验未变,最后批量 write;任一失败立即返回错误且未写过的文件保持原样

## 2. 注册

- [x] 2.1 `internal/tool/fs/register.go`:在 read/ls/glob/write/edit 后追加 `newMultiEditTool(root)`

## 3. 单测

- [x] 3.1 `multiedit_test.go`:
  - 单文件两处 edit happy path,验证 `Result.Files` 与 unified diff
  - 多文件 happy path,顺序无关
  - 同文件第二条 edit 依赖第一条应用后的内容(顺序敏感场景)
  - 零长 edits 数组 → 拒绝
  - 子项缺 path 或缺 old → 拒绝
  - 多匹配但未 replace_all → 拒绝
  - TOCTOU:在内存应用与二次 read 之间通过 `readFileFn` 注入差异 → 拒绝,且无任何文件被写
  - 其中一条 old 不匹配 → 整体拒绝,**未涉及的文件保持原样**
  - 路径跳出 root → 拒绝
- [x] 3.2 更新 `register_test.go`:断言 6 个工具名

## 4. 文档

- [x] 4.1 `docs/design.md`:把"multiedit 计划中,未实现"文案改为已实现并简述行为
- [x] 4.2 `openspec/changes/add-multiedit-tool/specs/fs-tools/spec.md`:补充 `multiedit` Requirement 与 Scenarios

## 5. 验证

- [x] 5.1 `go test ./internal/tool/fs/...`
- [x] 5.2 `go test ./...`
- [x] 5.3 `make lint`
- [x] 5.4 `make build`
- [x] 5.5 `openspec validate add-multiedit-tool --strict`(若环境有此 CLI)
