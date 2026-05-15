## 1. 包与类型骨架

- [x] 1.1 创建 `internal/message/` 目录
- [x] 1.2 新增 `Role`、`BlockType` 字符串别名和常量
- [x] 1.3 新增 `Message` 结构体，字段为 `Role` 与 `[]ContentBlock`
- [x] 1.4 新增 `ContentBlock` 结构体，覆盖 `type/text/image_url/tool_use_id/tool_name/input/output/is_error`
- [x] 1.5 为所有导出类型、常量和方法添加简洁 Go doc

## 2. 构造函数

- [x] 2.1 实现 `New(role Role, content ...ContentBlock) Message`
- [x] 2.2 实现 `Text(role Role, text string) Message` 或等价文本消息构造函数
- [x] 2.3 实现 `TextBlock(text string) ContentBlock`
- [x] 2.4 实现 `ImageBlock(url string) ContentBlock`
- [x] 2.5 实现 `ToolUseBlock(id, name string, input json.RawMessage) ContentBlock`
- [x] 2.6 实现 `ToolResultBlock(id, output string, isError bool) ContentBlock`

## 3. Message 方法

- [x] 3.1 实现 `(m Message) Text() string`，只提取文本 block，多个文本以换行拼接
- [x] 3.2 实现 `(m Message) Append(content ...ContentBlock) Message`，返回追加后的消息且不修改原消息
- [x] 3.3 实现 `(m Message) Clone() Message`，深拷贝 content slice
- [x] 3.4 确保 `Clone()` 深拷贝每个 `ContentBlock.Input`

## 4. JSON 行为

- [x] 4.1 用 struct tags 固定 JSON 字段名为 snake_case
- [x] 4.2 用 `omitempty` 避免序列化未使用字段
- [x] 4.3 确保 `json.RawMessage` 的 `input` 序列化为 JSON 对象 / 数组 / 字面量，而不是字符串
- [x] 4.4 确保 role 和 block type 序列化为可读字符串

## 5. 单元测试

- [x] 5.1 测试用户文本消息构造
- [x] 5.2 测试 role 和 block type JSON 字符串形态
- [x] 5.3 测试文本 block 的 JSON `omitempty`
- [x] 5.4 测试 tool_use input 原样序列化和 JSON 往返
- [x] 5.5 测试 tool_result 的 `is_error`、`tool_use_id`、`output`
- [x] 5.6 测试混合 content 的 JSON 往返和顺序保持
- [x] 5.7 测试 `Text()` 单文本、混合 block、多文本换行、无文本
- [x] 5.8 测试 `Append()` 保序且不修改原消息
- [x] 5.9 测试 `Clone()` 对 content block 和 `json.RawMessage` 的深拷贝

## 6. 验证与收尾

- [x] 6.1 运行 `gofmt` / `gofumpt` 覆盖新增 Go 文件
- [x] 6.2 运行 `go test ./...`
- [x] 6.3 运行 `make lint`
- [x] 6.4 运行 `make build`
- [x] 6.5 确认本次没有新增外部依赖
