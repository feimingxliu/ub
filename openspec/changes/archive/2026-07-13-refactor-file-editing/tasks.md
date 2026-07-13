## 1. 补丁编辑核心

- [x] 1.1 实现 `apply_patch` 信封解析、严格上下文 hunk 匹配和路径冲突校验，并覆盖语法与歧义错误。
- [x] 1.2 实现 Add / Update / Delete / Move 的内存预演、Preview、TOCTOU 校验、失败回滚和 LSP 通知，并覆盖原子性与换行保持。
- [x] 1.3 把 `apply_patch` 注册为写类 fs 工具，更新注册和工具描述测试。

## 2. 运行时集成

- [x] 2.1 将 `apply_patch` 接入活动摘要、TUI diff 详情和 LSP rename 的应用提示。
- [x] 2.2 将补丁目标解析接入文件 checkpoint，并测试新增、删除与重命名后的 rewind 追踪。

## 3. 文档与验证

- [x] 3.1 同步主 fs spec、requirements、design、usage 和 roadmap 中的工具清单与编辑协议。
- [x] 3.2 运行格式化及相关 Go 测试、构建和检查，并处理回归。

## 4. 审查修复

- [x] 4.1 将 apply_patch 的 Preview 计划按 tool_use_id 绑定到 Execute，并拒绝审批后发生的文件变化。
- [x] 4.2 使用受 root 约束的文件句柄执行补丁 I/O，拒绝通过 symlink 离开 workspace。
- [x] 4.3 以同目录临时文件、同步和 rename 完成单文件写入，显式恢复 mode，并覆盖失败回滚。
- [x] 4.4 同步规格与设计，运行严格验证和完整测试。
