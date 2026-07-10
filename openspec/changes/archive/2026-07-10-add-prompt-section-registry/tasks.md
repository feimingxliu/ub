## 1. Prompt registry

- [x] 1.1 定义固定 section ID、状态、稳定性、内部 message 投影和导出 manifest 类型
- [x] 1.2 将 startup、execution mode、memory 与 no-tool prompt 构造统一接入 registry,保持现有消息内容与顺序
- [x] 1.3 增加 section/整体字符数、模型 token 估算、截断状态和可选 content 输出

## 2. CLI inspect

- [x] 2.1 注册 `ub prompt inspect` 命令及 `--json`、`--show-content`、`--model` 参数
- [x] 2.2 实现纯本地文本/JSON manifest 渲染,默认省略正文且不初始化 provider、tools、session 或 maintenance

## 3. 验证与文档

- [x] 3.1 增加 registry golden/行为测试,覆盖默认、plan、no-tool、disabled、无 Git和无 memory
- [x] 3.2 增加 CLI 测试,覆盖文本/JSON、默认脱敏、显式正文及无持久副作用
- [x] 3.3 更新 usage/design/requirements/roadmap 文档并运行格式化、focused tests、repo-wide tests、lint 与 build/check
