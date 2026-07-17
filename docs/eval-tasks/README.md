# ub Eval MVP tasks

这些任务用于 `ub eval --task <name>` 的真实模型行为评测。每次运行都会复制对应 fixture 到临时 workspace，并使用隔离的 XDG state/data；只运行受信任务，因为 task 中的验证命令会在临时 workspace 内直接执行。

```bash
ub eval --task source-navigation --provider <provider> --model <model>
ub eval --task compact-continuation --json --keep-workspace
```

MVP 任务有意保持小而可解释：通过条件优先使用文件、命令和 rollout 工具序列，而不是对自然语言回答做逐字匹配。真实模型具有随机性，单次失败表示产生了一个可诊断样本，不等同于统计结论。
