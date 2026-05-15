## Context

权限 modal 当前直接打印 `tool.FileDiff.UnifiedDiff`。这能满足最小审批，但对多文件变更缺少导航，对代码 diff 也没有语言感知。I-25 将 diff 渲染抽成独立组件，供 permission modal 和后续 rollout/diff UI 复用。

## Goals / Non-Goals

**Goals:**

- 新增 `internal/tui/diffview`，以 `[]tool.FileDiff` 为输入渲染 unified diff。
- 通过 Chroma 根据文件名选择 lexer，对 diff 内容做终端 ANSI 高亮。
- 多文件 diff 显示 file tab，并支持 next/prev/up/down 方向切换。
- permission modal 展开 preview 时复用 diffview。
- 单测覆盖 Go/Python/TypeScript 文件不 panic、多文件切换和空 diff。

**Non-Goals:**

- 不做 split view 双栏对照。
- 不实现行内编辑、鼠标选择或虚拟滚动。
- 不改变 tool.Preview/FileDiff 数据模型。

## Decisions

1. **diffview 接收 tool.FileDiff。** 复用现有 preview 数据结构，避免新增中间 diff 数据模型。组件只负责展示和选择当前文件。

2. **Chroma 使用 quick/terminal256 风格。** 直接输出 ANSI 序列，符合 Bubble Tea 文本渲染路径。备选方案是自定义正负行颜色，但无法满足“按语言高亮”的要求。

3. **按文件扩展名选择 lexer。** 优先用 `lexers.Match(path)`，找不到则 fallback 到 plaintext。这样对 `.go`、`.py`、`.ts` 等路径自然覆盖。

4. **modal 只转发按键。** `permissiondialog.Model` 持有 diffview model；按 `d` 展开后，左右/上下键交给 diffview 切换文件，保持审批按键语义不变。

## Risks / Trade-offs

- **Chroma 依赖较大** → 只在 TUI diffview 引入，不影响 provider/tool 核心逻辑。
- **ANSI 内容影响测试稳定性** → 单测断言关键文本和不 panic，不依赖完整 ANSI 快照。
- **长 diff 仍可能撑满屏幕** → 本迭代不做滚动；后续可在独立 diffview 内加入 viewport。
