package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/feimingxliu/ub/internal/app/ub/agent"
	"github.com/feimingxliu/ub/internal/pkg/core/config"
	"github.com/feimingxliu/ub/internal/pkg/core/execution"
	"github.com/feimingxliu/ub/internal/pkg/core/reasoning"
	"github.com/feimingxliu/ub/internal/pkg/llm/provider"
	"github.com/feimingxliu/ub/internal/pkg/runtime/hook"
	"github.com/feimingxliu/ub/internal/pkg/runtime/permission"
	"github.com/feimingxliu/ub/internal/pkg/tool"
	"github.com/feimingxliu/ub/internal/pkg/workspace/filehistory"
	"github.com/feimingxliu/ub/internal/pkg/workspace/rollout"
)

// cliSubagentRunner dispatches one child agent run for the `task` tool. It
// captures the parent agent's configuration so the child reuses provider
// and tool registry verbatim (independent context is provided by giving
// the child a fresh Request.History).
//
// The child does NOT:
//
//   - get its own rollout/session entry (display-only child activity is
//     mirrored into the parent turn instead)
//   - run user-turn hooks (tool hooks are inherited so blocking policies still
//     apply to child tool calls)
//   - keep state after RunSubagent returns
type cliSubagentRunner struct {
	factory          *agent.Factory
	provider         provider.Provider
	tools            *tool.Registry
	permission       *permission.Manager
	model            string
	mode             execution.Mode
	modeFunc         func() execution.Mode
	reasoningCfg     *reasoning.Config
	maxContextTokens int
	contextCfg       config.ContextConfig
	promptCfg        config.PromptConfig
	runtime          agent.RuntimeContext
	hooks            hook.Runner
	defaultMaxTurns  int
	workspaceRoot    string
	memoryMaxChars   int
	fileHistory      *filehistory.Manager
	rollout          rollout.Writer
	events           agent.EventSink
}

func (r *cliSubagentRunner) RunSubagent(ctx context.Context, prompt string, maxTurns int) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("subagent: prompt is required")
	}
	if maxTurns <= 0 {
		maxTurns = r.defaultMaxTurns
	}
	parentSessionID := tool.SessionIDFromContext(ctx)
	parentTurn := tool.AgentTurnFromContext(ctx)
	parentToolUseID := tool.ToolUseIDFromContext(ctx)
	sessionID := fmt.Sprintf("subagent-%d", time.Now().UnixNano())
	childEvents := r.subagentEventSink(ctx, parentSessionID, parentTurn, parentToolUseID, sessionID)
	r.emitSubagentActivity(ctx, parentSessionID, parentTurn, agent.Event{
		Type:            agent.EventActivity,
		ActivityKind:    agent.ActivityNotice,
		ParentToolUseID: parentToolUseID,
		SubagentID:      sessionID,
		Status:          "running",
		Summary:         "subagent started",
		Content:         subagentActivityDetail(parentToolUseID, sessionID),
	})
	child, err := r.newChildAgent(maxTurns, childEvents)
	if err != nil {
		r.emitSubagentActivity(ctx, parentSessionID, parentTurn, agent.Event{
			Type:            agent.EventActivity,
			ActivityKind:    agent.ActivityNotice,
			ParentToolUseID: parentToolUseID,
			SubagentID:      sessionID,
			Status:          "failed",
			Summary:         "subagent failed to start",
			Content:         err.Error(),
			IsError:         true,
		})
		return "", fmt.Errorf("subagent: build child: %w", err)
	}
	res, err := child.Run(ctx, agent.Request{
		SessionID: sessionID,
		Turn:      1,
		Prompt:    prompt,
	})
	status := "done"
	summary := "subagent completed"
	content := subagentActivityDetail(parentToolUseID, sessionID)
	if err != nil {
		status = "failed"
		summary = "subagent failed"
		content = joinNonEmpty(content, err.Error())
	}
	r.emitSubagentActivity(ctx, parentSessionID, parentTurn, agent.Event{
		Type:            agent.EventActivity,
		ActivityKind:    agent.ActivityNotice,
		ParentToolUseID: parentToolUseID,
		SubagentID:      sessionID,
		Status:          status,
		Summary:         summary,
		Content:         content,
		IsError:         err != nil,
	})
	return res.Text, err
}

func (r *cliSubagentRunner) newChildAgent(maxTurns int, events agent.EventSink) (*agent.Agent, error) {
	factory := r.factory
	if factory == nil {
		factory = agent.NewFactory(agent.Options{
			Provider:         r.provider,
			Tools:            r.tools,
			Permission:       r.permission,
			Model:            r.model,
			Mode:             r.currentMode(),
			ModeFunc:         r.modeFunc,
			Reasoning:        r.reasoningCfg,
			MaxContextTokens: r.maxContextTokens,
			Context:          r.contextCfg,
			Prompt:           r.promptCfg,
			Runtime:          r.runtime,
			Hooks:            r.hooks,
			WorkspaceRoot:    r.workspaceRoot,
			MemoryMaxChars:   r.memoryMaxChars,
			FileHistory:      r.fileHistory,
		})
	}
	return factory.New(func(opts *agent.Options) {
		opts.Rollout = nil
		opts.MaxTurns = maxTurns
		opts.LimitAsker = nil
		opts.Asker = nil
		opts.PlanMode = nil
		opts.Hooks = subagentHookRunner{inner: r.hooks}
		opts.SummaryProvider = nil
		opts.SummaryModel = ""
		opts.AutoMemoryProvider = nil
		opts.AutoMemoryModel = ""
		opts.Memory = config.MemoryConfig{}
		opts.MemoryAutoScheduler = nil
		opts.SubagentRunner = r
		opts.FileHistory = r.fileHistory
		opts.FileHistoryToolsOnly = true
		opts.Events = events
		opts.BackgroundEvents = nil
	})
}

func (r *cliSubagentRunner) subagentEventSink(ctx context.Context, parentSessionID string, parentTurn int, parentToolUseID, subagentID string) agent.EventSink {
	if r == nil || (r.events == nil && r.rollout == nil) {
		return nil
	}
	return func(event agent.Event) {
		observed, ok := decorateSubagentEvent(event, parentToolUseID, subagentID)
		if !ok {
			return
		}
		if r.events != nil {
			r.events(observed)
		}
		if observed.Type == agent.EventActivity && observed.ActivityKind != agent.ActivityThinking {
			r.appendSubagentActivity(ctx, parentSessionID, parentTurn, observed)
		}
	}
}

func decorateSubagentEvent(event agent.Event, parentToolUseID, subagentID string) (agent.Event, bool) {
	switch event.Type {
	case agent.EventActivity:
	case agent.EventToolPartialOutput:
	default:
		return agent.Event{}, false
	}
	event.ParentToolUseID = parentToolUseID
	event.SubagentID = subagentID
	if strings.TrimSpace(event.ToolUseID) != "" {
		event.ToolUseID = namespaceSubagentToolUseID(parentToolUseID, subagentID, event.ToolUseID)
	}
	return event, true
}

func namespaceSubagentToolUseID(parentToolUseID, subagentID, toolUseID string) string {
	parts := []string{"subagent"}
	if trimmed := strings.TrimSpace(parentToolUseID); trimmed != "" {
		parts = append(parts, safeActivityIDPart(trimmed))
	} else if trimmed := strings.TrimSpace(subagentID); trimmed != "" {
		parts = append(parts, safeActivityIDPart(trimmed))
	}
	parts = append(parts, safeActivityIDPart(toolUseID))
	return strings.Join(parts, ":")
}

func safeActivityIDPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

func (r *cliSubagentRunner) emitSubagentActivity(ctx context.Context, parentSessionID string, parentTurn int, event agent.Event) {
	if r == nil {
		return
	}
	if r.events != nil {
		r.events(event)
	}
	r.appendSubagentActivity(ctx, parentSessionID, parentTurn, event)
}

func (r *cliSubagentRunner) appendSubagentActivity(ctx context.Context, parentSessionID string, parentTurn int, event agent.Event) {
	if r == nil || r.rollout == nil || strings.TrimSpace(parentSessionID) == "" || parentTurn <= 0 || event.Type != agent.EventActivity {
		return
	}
	activity, err := rollout.Activity(parentSessionID, parentTurn, rollout.ActivityPayload{
		ActivityKind:    string(event.ActivityKind),
		ToolUseID:       event.ToolUseID,
		ToolName:        event.ToolName,
		ParentToolUseID: event.ParentToolUseID,
		SubagentID:      event.SubagentID,
		Status:          event.Status,
		Summary:         event.Summary,
		Content:         event.Content,
		Decision:        event.Decision,
		Source:          event.Source,
		Reason:          event.Reason,
		Allowed:         event.Allowed,
		IsError:         event.IsError,
	})
	if err != nil {
		r.emitSubagentError(err)
		return
	}
	if err := r.rollout.Append(ctx, activity); err != nil {
		r.emitSubagentError(err)
	}
}

func (r *cliSubagentRunner) emitSubagentError(err error) {
	if r == nil || r.events == nil || err == nil {
		return
	}
	r.events(agent.Event{
		Type:    agent.EventError,
		Content: fmt.Sprintf("record subagent activity: %v", err),
		IsError: true,
		Err:     err,
	})
}

func subagentActivityDetail(parentToolUseID, subagentID string) string {
	var lines []string
	if trimmed := strings.TrimSpace(parentToolUseID); trimmed != "" {
		lines = append(lines, "parent_task: "+trimmed)
	}
	if trimmed := strings.TrimSpace(subagentID); trimmed != "" {
		lines = append(lines, "subagent: "+trimmed)
	}
	return strings.Join(lines, "\n")
}

func joinNonEmpty(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, "\n")
}

func (r *cliSubagentRunner) currentMode() execution.Mode {
	if r == nil {
		return execution.ModeWork
	}
	if r.modeFunc != nil {
		if mode, err := execution.ParseMode(string(r.modeFunc())); err == nil {
			return mode
		}
	}
	if mode, err := execution.ParseMode(string(r.mode)); err == nil {
		return mode
	}
	return execution.ModeWork
}

type subagentHookRunner struct {
	inner hook.Runner
}

func (r subagentHookRunner) Run(ctx context.Context, event hook.Event) hook.Decision {
	if r.inner == nil {
		return hook.Decision{}
	}
	switch event.Kind {
	case hook.KindPreToolCall, hook.KindPostToolCall:
		return r.inner.Run(ctx, event)
	default:
		return hook.Decision{}
	}
}
