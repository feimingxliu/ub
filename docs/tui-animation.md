# TUI 动画与运行指示器

> 状态：v0.1 — 与 `design.md` §3 / §12 配套。目标是消除"长任务期间界面看似卡住"的感觉。

## 1. 问题与目标

当前 TUI（`internal/tui/`）在 agent 长任务期间的反馈是**静态文本**：

- `status_bar.go` 显示 `state: thinking / streaming / tool / shell / finalizing`，但只在状态切换时更新；同一状态停留十几秒时屏幕没有任何视觉变化。
- 消息列表里的 activity 行（`Thinking...` / `Reading file...` / `Compacting...` 等）也是静态的。

用户的主观感受是"卡住了"，即便后台仍在正常推进。

**目标**：在 footer 增加一条**独立的运行指示行**，承担三件事：

1. 持续动效（spinner）—— 证明 UI 还活着
2. 文字化的当前状态（thinking / streaming / tool / shell / finalizing）
3. 已耗时（elapsed），让"等了多久"有数

## 2. 设计

### 2.1 位置

紧贴 status bar **上方**，作为 footer 的倒数第二行。`status_bar` 保持在最底端，原有信息层级不变。

```
┌────────────────────────────────────────────┐
│ 消息列表                                     │
│ ...                                         │
│ (queued / picker / permission modal 区)     │
│ ⠹ Thinking · 3s · Reading file              │  ← 新增：运行指示行
│ model: ... │ effort: ... │ state: ... │ ... │  ← status bar
└────────────────────────────────────────────┘
```

### 2.2 显示条件

| 条件 | 显示 |
|---|---|
| `m.running == true` 且 state ∈ {thinking, streaming, tool, shell, finalizing} | 是 |
| `m.running == false`（idle） | 否 |
| state 为 `failed` / `error` | 否（错误已落到消息列表） |
| 处于 picker / permission modal 等待用户输入 | 仍显示（任务在后台等审批，也属于"还没结束"） |

### 2.3 行内容格式

```
{spinner} {state 文案} · {elapsed} [· {activity 摘要}]
```

- **spinner**：Braille 10 帧 `⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏`
- **state 文案**：直接复用 `status_bar.go` 的 `statusThinking` / `statusStreaming` / `statusTool` / `statusShell` / `statusFinalizing` 常量，首字母大写
- **elapsed**：从当次 run 启动（`startPrompt` / `startShell` / `startCompact`）记录的 `runStartedAt` 算起
  - <60s：`12s`
  - <60min：`3m12s`
  - ≥60min：`1h03m`
- **activity 摘要**：最近一条 `EventActivity` 的 `Summary` 字段，截断到剩余宽度

举例：

```
⠹ Thinking · 3s
⠼ Tool · 12s · Reading file internal/tui/model.go
⠴ Streaming · 5s
⠧ Shell · 1s
⠦ Finalizing · 18s
```

### 2.4 帧与节奏

- tick 间隔 **80ms**（≈ 12fps），Braille spinner 的常见值
- 由 `tea.Tick` 驱动
- **仅 `m.running` 为 true 时续 tick**——idle/error 自然停止，CPU/电量友好

### 2.5 颜色

复用 `tuitheme` 现有色板，与 state 段保持一致：

- 默认前景 = `styles.Status.StateBusy.GetForeground()`
- error/failed 不显示，故无需 error 配色

不引入新的 theme 字段。

## 3. 实现

### 3.1 新增文件 `internal/tui/spinner.go`

承担：

- 帧序列常量 `spinnerFrames`
- 消息类型 `spinnerTickMsg`
- 命令构造 `spinnerTickCmd() tea.Cmd`，用 `tea.Tick(spinnerTickInterval, ...)`
- 计时格式化 `formatElapsed(time.Duration) string`

不放进 model.go，单独成文件便于将来抽离/复用。

### 3.2 `model.go` 改动

新增字段：

```go
type Model struct {
    // ... 既有字段
    spinnerFrame    int
    runStartedAt    time.Time
    activitySummary string
}
```

`Update` 增加分支：

```go
case spinnerTickMsg:
    if !m.running {
        return m, nil          // 不再续 tick
    }
    m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
    return m, spinnerTickCmd()
```

`startPrompt` / `startShell` / `startCompact` 在已有 `tea.Batch` 里追加 `spinnerTickCmd()`，并在切换 `m.running = true` 同时设置 `m.runStartedAt = time.Now()`、`m.spinnerFrame = 0`、`m.activitySummary = ""`。

`waitForEventFromUpdate` 中 `EventActivity` 分支顺手更新 `m.activitySummary = event.Summary`。

### 3.3 `frame.go` 改动

`footerFrame` 在 `m.status.view(...)` 之前插入：

```go
if line := m.runIndicatorView(width); line != "" {
    lines = append(lines, splitFrameLines(line)...)
}
```

`runIndicatorView` 实现在 `spinner.go` 或 `model.go`，根据 `m.running` / `m.status.state` 决定是否返回内容。

### 3.4 测试

新建 `spinner_test.go`，覆盖：

- `formatElapsed` 在 30s / 90s / 3700s 三档的格式
- `runIndicatorView` 在 `running=false`、`running=true & state=thinking`、`state=failed` 三种状况下的输出（前者返回 ""，第二种含 spinner 字符，第三种返回 ""）

不为 tick loop 写测试（依赖 Bubble Tea runtime，性价比低）。

## 4. 非目标 / 留待 V2

- 不做 activity 消息组内联 spinner（消息列表里的 `Thinking...` 行保持静态）—— 底栏指示器已经覆盖"全局是否在动"的需求
- 不做进度条（无可靠的进度来源）
- 不做按 state 切换 spinner 字符（统一 Braille，state 文案区分阶段已足够）
- 不做 idle 时的"心跳" 动画（idle 就该静止）

## 5. 验收

- 启动 ub，发一条会触发 tool 调用的 prompt
- footer 应在 status bar 上方多出一行 `⠋ Thinking · 0s` 并持续动起来
- tool 触发后变为 `⠹ Tool · Ns · {summary}`
- 任务结束（或被 Esc/Ctrl+C 中断）后该行立即消失
- 退出 TUI 后无 goroutine 泄漏（tick 由 Bubble Tea runtime 管理，无需手动清理）
