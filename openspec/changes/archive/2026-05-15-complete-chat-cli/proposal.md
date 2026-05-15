## Why

Sprint 1 的最后一步需要把 `ub chat` 从单轮 smoke 命令补齐为可继续 session 的实用入口。这样后续 agent loop/TUI 接入前，provider、rollout 和 session 已有稳定的 CLI 验证路径。

## What Changes

- `ub chat` 新增 `--session <id>`，在已有 session 上继续并把历史消息叠加到 provider request。
- `ub chat` 新增 `--new`，在存在默认/最近 session 时也强制创建新 session。
- 明确 `--provider` / `--model` 为本次调用临时覆盖，不写回配置。
- session 继续时更新 session metadata 与 `updated_at`。
- 改善 provider 不存在、session 不存在、provider 鉴权/模型错误的用户可读错误。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `provider-runtime`: `ub chat` 从最小单轮命令扩展为支持 session 继续、新建控制和更清晰错误的 CLI。
- `rollout-events`: `ub chat --session` 会读取已有 rollout 消息作为历史，并把新一轮事件追加到同一 session。

## Impact

- 修改 `internal/cli` chat 参数解析、session 选择、history 重建和错误处理。
- 不修改 provider 接口，不实现 TUI `/model` 命令。
