## Context

当前仓库已有 CLI、配置、store 和日志基础，但还没有内部消息表示。后续 fake provider、真实 provider 转换、agent loop、rollout 事件和 context 压缩都会传递消息；如果直接复用某个 SDK 类型，会把核心包绑定到单一 provider。

## Goals / Non-Goals

**Goals:**

- 在 `internal/message` 定义 SDK 无关的 `Message`、`Role`、`ContentBlock`。
- 支持 `text`、`image`、`tool_use`、`tool_result` 四类 content block。
- JSON 字段稳定、可往返序列化，适合后续 rollout 作为 payload 持久化。
- 提供小而明确的工具方法：`Text()`、`Append(content)`、`Clone()`。
- 用单元测试锁定 JSON、深拷贝、文本提取和 append 行为。

**Non-Goals:**

- 不做 Anthropic/OpenAI/Ollama/provider 转换。
- 不实现 provider 接口、fake provider、agent loop、tool registry 或 rollout writer。
- 不校验图片 URL/base64 格式，不解析 tool input schema。
- 不引入外部依赖或泛型抽象。

## Decisions

### 1. 单一 ContentBlock 列表

`Message` 只包含 `Role Role` 和 `Content []ContentBlock`。不额外维护 `ToolCalls []...` 或 `ToolResults []...` 平行字段，避免同一事实出现两个来源。

备选方案是为 tool call / result 做独立字段，转换 provider 时可能更接近部分 SDK；但这会让消息顺序变得模糊，也不利于 rollout 原样重放。统一 content block 能保留 assistant 文本、工具调用、工具结果的自然顺序。

### 2. Role 与 BlockType 使用字符串别名

定义：

```go
type Role string
type BlockType string
```

常量包括 `RoleUser`、`RoleAssistant`、`RoleSystem`、`RoleTool` 和 `BlockText`、`BlockImage`、`BlockToolUse`、`BlockToolResult`。使用字符串别名可保持 JSON 可读性，也允许未来 provider 适配层在必要时容忍新值。

### 3. ContentBlock 字段形态

`ContentBlock` 使用 roadmap 的字段作为基线：

```go
type ContentBlock struct {
    Type      BlockType       `json:"type"`
    Text      string          `json:"text,omitempty"`
    ImageURL  string          `json:"image_url,omitempty"`
    ToolUseID string          `json:"tool_use_id,omitempty"`
    ToolName  string          `json:"tool_name,omitempty"`
    Input     json.RawMessage `json:"input,omitempty"`
    Output    string          `json:"output,omitempty"`
    IsError   bool            `json:"is_error,omitempty"`
}
```

图片只保留 `image_url`，V1 不表达 mime/base64。`Input` 保持 `json.RawMessage`，这样工具参数不被内部消息包解析或重排。

### 4. 工具方法语义

- `Text()`：按 content 顺序拼接所有 `text` block 的 `Text`，用空字符串跳过非文本 block；多个文本块以换行拼接。
- `Append(content ...ContentBlock)`：返回追加后的 `Message`，可选择值接收者，避免调用方误以为会修改原消息。
- `Clone()`：深拷贝 `Content` slice 和每个 block 的 `Input` raw bytes，保证调用方修改 clone 不影响原始消息。

## Risks / Trade-offs

- [Risk] 过早固定 block 字段可能不覆盖所有 provider 特性 → 保持字段最小，只表达 V1 需要；provider 特有字段留给适配层处理。
- [Risk] `Text()` 对多文本块的分隔符约定影响提示拼接 → 明确使用换行拼接，并用测试锁定。
- [Risk] `json.RawMessage` 可能被调用方传入非法 JSON → 构造 helper 可做基础校验；底层 struct 仍允许零值以方便测试和解码。
