## Context

`docs/roadmap.md I-17` 明确：优先调外部 ripgrep，缺失时回落到内置 `regexp` + 文件遍历，输出 `path:line:match`，并显式排除 Sourcegraph 远程搜索。`add-fs-tools` 已经在 `internal/tool/fs/path.go` 落地了 workspace 沙箱函数 `resolve(root, path)`，本 change 直接复用同一份规则，保证所有本地工具的路径校验语义一致。

## Goals / Non-Goals

**Goals:**

- 实现一个 `grep` 工具，覆盖 V1 最常用的“按正则搜代码”场景。
- 输出格式与“是否走 rg”解耦：两个后端 MUST 返回完全相同的文本，LLM 不需要感知实现。
- 后端选择可注入，测试不依赖系统是否安装 rg。
- 严格沿用沙箱：拒绝跳出 root；二进制文件跳过避免污染输出。

**Non-Goals:**

- 不实现多行匹配、上下文行、PCRE 高级特性。
- 不实现 Sourcegraph 等远程搜索。
- 不做并发 walker（V1 单 goroutine，内置实现走顺序）。
- 不处理 .gitignore 之外的过滤策略；rg 默认会读 `.gitignore`，内置实现 V1 不解析 `.gitignore`，差异在 design 与 spec 中明确写出。
- 不实现 bash 工具（I-18）、权限审批 / dispatcher（I-20/I-21）。

## Decisions

- **包结构：** 全部放在 `internal/tool/search/`：`grep.go`（Tool 实现）、`backend.go`（后端接口与两种实现）、`register.go`（`Register` 入口）、`*_test.go` 单测。和 `internal/tool/fs/` 的风格保持一致。
- **后端接口：**
  ```go
  type backend interface {
      run(ctx context.Context, root string, opts grepOpts) ([]grepHit, error)
  }
  type grepHit struct{ Path string; Line int; Text string }
  ```
  - `rgBackend`：用 `os/exec` 调 `rg --line-number --no-heading --color=never --no-messages PATTERN PATH`，按 `path:line:match` 解析输出。
  - `goBackend`：用 `filepath.WalkDir` + `regexp.Regexp` + `bufio.Scanner`。
- **后端选择：** `Register(reg, root)` 内部按以下顺序选择：
  1. 优先使用 `goBackend`（V1 保守起见，先把 deterministic 路径走通；rg 后端默认启用与否在后续 iteration 由 config 控制，本 change 实现 rg 后端代码但默认不启用）。
  2. 通过包内 `var resolveBackend func(...) backend` 注入点暴露给测试。
  说明：V1 默认 `goBackend` 是为了避免“开发机有 rg、CI 没有”导致行为漂移。rg 后端代码 + 测试一起交付，留给后续 iteration 加 config 开关启用，本 change spec 显式声明默认是内置实现。
- **input schema：**
  - `pattern string`（必填，正则；`regexp.Compile` 失败时返回错误）。
  - `path string`（可选，相对 root 的子目录；默认 `.`）。
  - `include string`（可选，doublestar glob；不传则不过滤；过滤时对“相对 root 的路径”用 `doublestar.PathMatch`）。
- **输出格式：** 每行 `path:line:match`，path 相对 root，line 是 1-based，match 是该行原文（保留前后空白，不做截断）。多条结果按 `path` 升序、同文件内按 `line` 升序排序，让 LLM 看到 deterministic 顺序。无匹配返回空字符串 + `IsError=false`。
- **二进制检测：** 读文件前先 `os.Open` + 读首 8KB，若包含 `\x00` 字节判为二进制并跳过；这是 ripgrep 的默认策略，内置实现对齐。rg 后端不需要额外处理（rg 自己跳）。
- **沙箱：** `path` 子目录入参经过 `fs.resolve(root, path)`；为了避免在 `internal/tool/search/` 反向依赖 `internal/tool/fs/`，把 `resolve` 从 `internal/tool/fs/path.go` 中提取到 `internal/tool/path.go` 公共位置。**前置改动：** 本 change 在实现前先在 `internal/tool/` 加 `path.go` 暴露 `Resolve(root, path) (string, error)`，并把 `fs` 包改为复用同一份实现（不改 `fs-tools` spec，仅是内部重构）。该重构在 tasks.md 的 “0. 共享 path helper 提升” 段记录。
- **`include` 实现：** 在 walker 命中文件前用 `doublestar.PathMatch(include, relPath)` 过滤；rg 后端通过 `-g <include>` 透传。
- **路径相对化：** `goBackend` 用 `filepath.Rel(root, abs)`，并把所有 `\` 替换为 `/`，保证跨平台 deterministic（Windows 不在 V1 必须支持范围，但简单替换不引入风险）。
- **超大行截断：** 单行超过 2KB 时按 2KB 截断并在末尾加 ` ...(truncated)`，避免污染上下文窗口；ripgrep 默认行限制更大，但 V1 统一在解析层做后处理。

## Risks / Trade-offs

- **rg 默认禁用** → 本机用户的搜索可能比 rg 慢，但 V1 是学习向项目、单测/CI 行为优先于性能；性能开关留给后续 iteration。
- **内置实现不读 `.gitignore`** → 会搜到 `node_modules` 这类大目录；V1 用户可以靠 `include` 过滤，或在后续 iteration 加 `.gitignore` 解析。spec 中显式写明这一点。
- **二进制文件检测只看首 8KB** → 与 ripgrep 行为一致；极少数大文件前 8KB 全是文本但后续有 NUL 的会被错认为文本，V1 接受这个风险。
- **rg 后端代码先实现但默认不启用** → 多一份代码维护成本，但能避免后续 iteration 再写一遍；测试通过注入后端覆盖，避免 dead code 风险。
