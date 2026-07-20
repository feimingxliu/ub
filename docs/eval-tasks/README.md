# ub Eval MVP tasks

这些任务用于 `ub eval --task <name>` 的真实模型行为评测。每次运行都会复制对应 fixture 到临时 workspace，并使用隔离的 XDG state/data；只运行受信任务，因为 task 中的验证命令会在临时 workspace 内直接执行。

```bash
ub eval --task source-navigation --provider <provider> --model <model>
ub eval --task compact-continuation --json --keep-workspace
ub eval \
  --task source-navigation --task read-before-edit \
  --target vibecoding=openai/glm-5.2 \
  --target vibecoding=openai/deepseek-v4-flash \
  --repeat 3 --parallel 2 --json
```

MVP 任务有意保持小而可解释：通过条件优先使用文件、命令和 rollout 工具序列，而不是对自然语言回答做逐字匹配。真实模型具有随机性，单次失败表示产生了一个可诊断样本，不等同于统计结论。

多个 task/target 或重复样本会输出 `kind: matrix` 报告，完整保留每次单任务 report，并按 overall、target、task 聚合。显式 target 使用 `provider=model`，因此 model ID 中的 `/` 会原样保留，不会和 provider 自动做交叉积。target 首样本若在没有 agent 行为前发生 infrastructure failure，会熔断该 target 并把剩余计划样本标为 skipped；这类 skipped 和普通 assertion/agent failure 分开统计。

Task 可选的 `runtime` 只允许覆盖 Eval 可重复性所需的 context 参数：

```yaml
runtime:
  max_context_tokens: 30000
  context:
    trigger_ratio: 0.5
    keep_recent_turns: 1
```

这些值通过当前 eval 子进程的隐藏参数应用，不会改写用户配置或复制 provider 凭据；文本和 JSON 报告会回显实际声明的覆盖。`compact-continuation` 使用这组边界确保 follow-up 前后的历史进入 `ContextDecision=compact`，不依赖模型原生窗口大小。
