## 1. shell_metadata 块

- [x] 1.1 `bash.go` `assembleContent`:输出从平铺改为
  ```
  <shell_metadata>
  exit_code=N
  duration_ms=M
  [timeout=true]
  [aborted=true]
  [error=<msg>]
  </shell_metadata>
  --- stdout ---
  ...
  --- stderr ---
  ...
  ```
- [x] 1.2 timeout 与 aborted 用不同 `killReason` 前缀区分:`timeout after ...` → timeout、`cancelled: ...` → aborted
- [x] 1.3 `bash_test.go`:happy path、timeout、aborted(ctx 取消)分别断言新 block 存在与对应 flag

## 2. full_max_bytes 上限

- [x] 2.1 `config/types.go` `ContextToolResultConfig` 新增 `FullMaxBytes int`、`SpilloverDir string`
- [x] 2.2 `tooloutput.EffectiveLimits`:`FullMaxBytes` 默认 50 * 1024 * 1024
- [x] 2.3 `tooloutput.LimitResult` / `writeSpillover`:
  - 若 `FullMaxBytes > 0 且 len(full) > FullMaxBytes` → 截断到 `validPrefix(full, FullMaxBytes)`
  - 在截断后的尾部追加 `\n... [spillover truncated: original_bytes=N kept=M]\n`
  - footer 仍含正确的 `original_bytes`(= 截前长度)
- [x] 2.4 `tooloutput_test.go`:写一条大于 cap 的内容,确认落盘文件 ≤ cap + footer,且 result.Content 中 `original_bytes` 等于真实原长

## 3. spillover_dir 配置

- [x] 3.1 `tooloutput.Limits` 新增 `SpilloverDir string`;`OutputRoot(stateRoot)` 行为不变,但 `LimitResult` 在 `opts.Limits.SpilloverDir != ""` 时优先用它
- [x] 3.2 `EffectiveLimits` 从 `cfg.ToolResults.SpilloverDir` 取值
- [x] 3.3 测试:配 SpilloverDir 后落盘路径前缀正确

## 4. 文档

- [x] 4.1 `docs/usage.md`:新增小节"长输出落盘",讲 metadata 块与三个 config 字段
- [x] 4.2 `docs/design.md`:在 bash 工具描述里链上 shell_metadata
- [x] 4.3 `openspec/changes/refine-output-spillover/specs/output-spillover/spec.md`

## 5. 验证

- [x] 5.1 `go test ./...`
- [x] 5.2 `make lint`
- [x] 5.3 `make build`
- [x] 5.4 `make schema`(因 ContextToolResultConfig 多字段)
