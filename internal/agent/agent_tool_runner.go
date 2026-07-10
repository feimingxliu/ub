package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/feimingxliu/ub/internal/hook"
	"github.com/feimingxliu/ub/internal/permission"
	"github.com/feimingxliu/ub/internal/tool"
	"github.com/feimingxliu/ub/internal/workspace/tooloutput"
)

// runTool executes a single tool call through the full lifecycle: pre-tool
// hooks, permission checks, preview generation, execution (streaming or
// plain), result limiting, post-tool hooks, and rollout/activity recording.
// It always returns a tool.Result — errors from the tool itself are wrapped
// into an IsError result rather than propagated as Go errors, so the agent
// loop can feed them back to the model as tool_result content.
func (a *Agent) runTool(ctx context.Context, sessionID string, turn int, call toolCall) tool.Result {
	ctx = tool.WithSessionID(ctx, sessionID)
	ctx = tool.WithAgentTurn(ctx, turn)
	ctx = tool.WithToolUseID(ctx, call.ID)
	ctx = contextWithAsker(ctx, a.asker)
	ctx = contextWithPlanModeController(ctx, a.planMode)
	if a.subagentRunner != nil {
		ctx = tool.WithSubagentRunner(ctx, a.subagentRunner)
	}
	preDec := a.hooks.Run(ctx, hook.Event{
		Kind:      hook.KindPreToolCall,
		SessionID: sessionID,
		ToolName:  call.Name,
		ToolUseID: call.ID,
		ToolArgs:  call.Input,
	})
	a.emitHookOutcomes(preDec)
	if preDec.Block {
		reason := strings.TrimSpace(preDec.Reason)
		if reason == "" {
			reason = "pre_tool_call hook blocked"
		}
		result := tool.Result{Content: fmt.Sprintf("pre_tool_call hook blocked %s: %s", call.Name, reason), IsError: true}
		summary, detail := ToolActivityResultWithInput(call.Name, call.Input, result)
		a.emitToolActivity(call, "blocked", summary, detail, true)
		return result
	}
	t, ok := a.tools.Get(call.Name)
	if !ok {
		result := tool.Result{Content: fmt.Sprintf("tool %q not found", call.Name), IsError: true}
		summary, detail := ToolActivityResultWithInput(call.Name, call.Input, result)
		a.emitToolActivity(call, "failed", summary, detail, true)
		return result
	}
	if !toolAvailableInMode(call.Name, a.currentMode()) {
		slog.Warn("tool blocked by mode", "session", sessionID, "turn", turn, "tool", call.Name, "mode", a.currentMode())
		result := tool.Result{Content: toolUnavailableInModeMessage(call.Name, a.currentMode()), IsError: true}
		summary, detail := ToolActivityResultWithInput(call.Name, call.Input, result)
		a.emitToolActivity(call, "failed", summary, detail, true)
		return result
	}
	var preview *tool.Preview
	if previewable, ok := t.(tool.PreviewableTool); ok {
		pv, err := previewable.Preview(ctx, call.Input)
		if err != nil {
			result := tool.Result{Content: fmt.Sprintf("preview %q: %v", call.Name, err), IsError: true}
			summary, detail := ToolActivityResultWithInput(call.Name, call.Input, result)
			a.emitToolActivity(call, "failed", summary, detail, true)
			return result
		}
		preview = &pv
	}
	a.emitToolActivity(call, "running", SummarizeToolInput(call.Name, call.Input), ToolInputDetail(call.Name, call.Input), false)
	if call.Name == "ask" {
		a.recordAskActivity(ctx, sessionID, turn, call, "requested", askActivityRequestContent(call.Input), false)
	}
	if a.permission != nil {
		approvalObserved := false
		result, err := a.permission.Ask(ctx, permission.Request{
			Tool:             call.Name,
			Args:             call.Input,
			Risk:             t.Risk(),
			Mode:             a.currentMode(),
			Preview:          preview,
			Workspace:        a.workspaceRoot,
			ApprovalObserver: a.permissionObserver(ctx, sessionID, turn, call.Name, &approvalObserved),
		})
		if err != nil {
			a.recordPermissionActivity(ctx, sessionID, turn, call.Name, "permission", "error", err.Error(), false)
			result := tool.Result{Content: fmt.Sprintf("permission %q: %v", call.Name, err), IsError: true}
			summary, detail := ToolActivityResultWithInput(call.Name, call.Input, result)
			a.emitToolActivity(call, "failed", summary, detail, true)
			return result
		}
		if (t.Risk() == tool.RiskExec || t.Risk() == tool.RiskNetwork || !result.Allowed) && !(approvalObserved && result.Source == permission.SourceApprovalAgent) {
			a.recordPermissionActivity(ctx, sessionID, turn, call.Name, string(result.Source), string(result.Decision), result.Reason, result.Allowed)
		}
		if !result.Allowed {
			slog.Info("tool permission denied", "session", sessionID, "turn", turn, "tool", call.Name, "source", result.Source, "reason", result.Reason)
			reason := strings.TrimSpace(result.Reason)
			if reason == "" {
				reason = string(result.Decision)
			}
			result := tool.Result{Content: fmt.Sprintf("permission denied for %q: %s", call.Name, reason), IsError: true}
			summary, detail := ToolActivityResultWithInput(call.Name, call.Input, result)
			a.emitToolActivity(call, "failed", summary, detail, true)
			return result
		}
	}
	if a.fileHistory != nil {
		if err := a.fileHistory.TrackTool(ctx, call.Name, call.Input); err != nil {
			a.emit(Event{Type: EventError, Content: fmt.Sprintf("file history track %s: %v", call.Name, err), IsError: true, Err: err})
		}
	}
	result, err := a.executeToolCall(ctx, t, call)
	if err != nil {
		slog.Warn("tool execution failed", "session", sessionID, "turn", turn, "tool", call.Name, "err", err)
		result := tool.Result{Content: err.Error(), IsError: true}
		result = a.limitToolResult(sessionID, call, result)
		summary, detail := ToolActivityResultWithInput(call.Name, call.Input, result)
		a.emitToolActivity(call, "failed", summary, detail, true)
		return result
	}
	result = a.limitToolResult(sessionID, call, result)
	a.emitHookOutcomes(a.hooks.Run(ctx, hook.Event{
		Kind:      hook.KindPostToolCall,
		SessionID: sessionID,
		ToolName:  call.Name,
		ToolUseID: call.ID,
		ToolArgs:  call.Input,
		Result:    &result,
	}))
	status := "done"
	if result.IsError {
		status = "failed"
	}
	if call.Name == "ask" {
		askStatus := "answered"
		if result.IsError {
			askStatus = "failed"
		} else if strings.EqualFold(strings.TrimSpace(result.Metadata["ask_status"]), "skipped") {
			askStatus = "skipped"
		} else if strings.EqualFold(strings.TrimSpace(result.Metadata["ask_status"]), "unavailable") {
			askStatus = "unavailable"
		}
		a.recordAskActivity(ctx, sessionID, turn, call, askStatus, result.Content, result.IsError)
	}
	if call.Name == "enter_plan_mode" || call.Name == "exit_plan_mode" {
		a.recordModeActivity(ctx, sessionID, turn, call, result)
	}
	if call.Name == "create_goal" || call.Name == "update_goal" || call.Name == "get_goal" {
		a.recordGoalActivity(ctx, sessionID, turn, call, result)
	}
	summary, content := ToolActivityResultWithInput(call.Name, call.Input, result)
	a.emitToolActivity(call, status, summary, content, result.IsError)
	if call.Name == "remember" && !result.IsError {
		a.recordRememberToolMemoryWrite(ctx, sessionID, turn, result)
	}
	return result
}

// streamPartialMaxBytes caps each forwarded chunk so a chatty tool can't
// flood the event sink. Chunks beyond this size get a tail marker.
const streamPartialMaxBytes = 4 * 1024

// executeToolCall picks the streaming path for tools that opt in, plain
// Execute otherwise. For streaming tools, each StreamEvent emitted while
// the tool is running is forwarded into the agent EventSink as an
// EventToolPartialOutput so the TUI can render running progress without
// waiting for the final Result.
func (a *Agent) executeToolCall(ctx context.Context, t tool.Tool, call toolCall) (tool.Result, error) {
	streamer, ok := t.(tool.StreamingTool)
	if !ok {
		return t.Execute(ctx, call.Input)
	}
	events := make(chan tool.StreamEvent, 64)
	resultCh := make(chan struct {
		res tool.Result
		err error
	}, 1)
	go func() {
		var res tool.Result
		var err error
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("streaming tool %s panic: %v", call.Name, r)
				res = tool.Result{Content: err.Error(), IsError: true}
			}
			closeToolStreamEvents(events)
			resultCh <- struct {
				res tool.Result
				err error
			}{res: res, err: err}
		}()
		res, err = streamer.ExecuteStream(ctx, call.Input, events)
	}()
	summary := SummarizeToolInput(call.Name, call.Input)
	for ev := range events {
		data := ev.Data
		if len(data) > streamPartialMaxBytes {
			data = data[:streamPartialMaxBytes] + " ... [chunk truncated]"
		}
		a.emit(Event{
			Type:      EventToolPartialOutput,
			ToolUseID: call.ID,
			ToolName:  call.Name,
			Status:    string(ev.Kind),
			Summary:   summary,
			Content:   data,
		})
	}
	r := <-resultCh
	return r.res, r.err
}

func closeToolStreamEvents(events chan tool.StreamEvent) {
	defer func() { _ = recover() }()
	close(events)
}

// limitToolResult applies the tooloutput size limits to a tool result,
// spilling oversized content to disk and replacing it with a reference.
// If spillover fails (e.g. disk full), it falls back to inline truncation
// so the agent loop is never blocked by a storage error.
func (a *Agent) limitToolResult(sessionID string, call toolCall, result tool.Result) tool.Result {
	limited, err := tooloutput.LimitResult(result, tooloutput.LimitOptions{
		SessionID: sessionID,
		ToolUseID: call.ID,
		StateRoot: a.toolOutputState,
		Limits:    tooloutput.EffectiveLimits(a.contextCfg),
	})
	if err == nil {
		return limited
	}
	fallbackLimits := tooloutput.EffectiveLimits(a.contextCfg)
	fallbackLimits.SpilloverEnabled = false
	limited, fallbackErr := tooloutput.LimitResult(result, tooloutput.LimitOptions{
		Limits: fallbackLimits,
	})
	if fallbackErr != nil {
		return tool.Result{Content: fmt.Sprintf("tool result limiting failed: %v", fallbackErr), IsError: true}
	}
	if limited.Content != "" {
		limited.Content += "\n"
	}
	limited.Content += fmt.Sprintf("spillover_error=%v", err)
	return limited
}

// permissionObserver returns a callback that records approval-agent
// decisions as permission activity events in the rollout. It is invoked
// synchronously from within permission.Manager.Ask when the auto-mode
// approval agent makes a decision.
func (a *Agent) permissionObserver(ctx context.Context, sessionID string, turn int, toolName string, observed *bool) func(permission.ApprovalObservation) {
	return func(obs permission.ApprovalObservation) {
		if observed != nil {
			*observed = true
		}
		decision := strings.TrimSpace(obs.Decision)
		reason := strings.TrimSpace(obs.Reason)
		if obs.Err != nil {
			decision = "error"
			reason = obs.Err.Error()
		}
		a.recordPermissionActivity(ctx, sessionID, turn, toolName, string(permission.SourceApprovalAgent), decision, reason, obs.Err == nil && decision == "allow")
	}
}
