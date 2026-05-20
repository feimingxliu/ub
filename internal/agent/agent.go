// Package agent implements ub's headless provider/tool loop.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/feimingxliu/ub/internal/config"
	contextmgr "github.com/feimingxliu/ub/internal/context"
	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/permission"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/reasoning"
	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/tool"
	"github.com/feimingxliu/ub/internal/tooloutput"
)

const defaultMaxTurns = 25

// ErrMaxTurns is returned when a run exceeds its provider/tool loop limit.
var ErrMaxTurns = errors.New("agent: max turns reached")

const maxTurnsFinalInstruction = "Tool iteration limit reached for this turn. Do not call tools. Answer the user's request now using the information already gathered. If the available information is incomplete, say what is missing concisely."

// Options configures an Agent.
type Options struct {
	Provider         provider.Provider
	Tools            *tool.Registry
	Permission       *permission.Manager
	Rollout          rollout.Writer
	Model            string
	Mode             execution.Mode
	ModeFunc         func() execution.Mode
	MaxTurns         int
	Events           EventSink
	Reasoning        *reasoning.Config
	MaxContextTokens int
	SummaryProvider  provider.Provider
	SummaryModel     string
	Context          config.ContextConfig
	Runtime          RuntimeContext
	ToolOutputState  string
}

// Agent runs a single headless agent loop.
type Agent struct {
	provider         provider.Provider
	tools            *tool.Registry
	permission       *permission.Manager
	rollout          rollout.Writer
	model            string
	mode             execution.Mode
	modeFunc         func() execution.Mode
	maxTurns         int
	events           EventSink
	reasoning        *reasoning.Config
	maxContextTokens int
	summaryProvider  provider.Provider
	summaryModel     string
	contextCfg       config.ContextConfig
	runtime          RuntimeContext
	toolOutputState  string
}

// Request is one Agent run input.
type Request struct {
	SessionID string
	Turn      int
	History   []message.Message
	Prompt    string
}

// Result is the final Agent run output.
type Result struct {
	Text     string
	Messages []message.Message
}

type toolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

type streamResult struct {
	text      string
	message   message.Message
	toolCalls []toolCall
}

// New constructs an Agent.
func New(opts Options) (*Agent, error) {
	if opts.Provider == nil {
		return nil, errors.New("agent provider is required")
	}
	if opts.Tools == nil {
		return nil, errors.New("agent tool registry is required")
	}
	mode, err := execution.ParseMode(string(opts.Mode))
	if err != nil {
		return nil, err
	}
	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxTurns
	}
	toolOutputState := strings.TrimSpace(opts.ToolOutputState)
	if toolOutputState == "" {
		if stateRoot, err := tooloutput.StateRoot(); err == nil {
			toolOutputState = stateRoot
		}
	}
	return &Agent{
		provider:         opts.Provider,
		tools:            opts.Tools,
		permission:       opts.Permission,
		rollout:          opts.Rollout,
		model:            strings.TrimSpace(opts.Model),
		mode:             mode,
		modeFunc:         opts.ModeFunc,
		maxTurns:         maxTurns,
		events:           opts.Events,
		reasoning:        cloneReasoning(opts.Reasoning),
		maxContextTokens: opts.MaxContextTokens,
		summaryProvider:  opts.SummaryProvider,
		summaryModel:     strings.TrimSpace(opts.SummaryModel),
		contextCfg:       opts.Context,
		runtime:          opts.Runtime.normalized(),
		toolOutputState:  toolOutputState,
	}, nil
}

// Run executes one user prompt.
func (a *Agent) Run(ctx context.Context, req Request) (Result, error) {
	if req.Turn <= 0 {
		req.Turn = 1
	}
	userMsg := message.Text(message.RoleUser, req.Prompt)
	messages := cloneMessages(req.History)
	messages = append(messages, userMsg)
	if err := a.append(ctx, req.SessionID, func() (rollout.Event, error) {
		return rollout.UserMessage(req.SessionID, req.Turn, userMsg)
	}); err != nil {
		return Result{}, err
	}

	tools, err := toolDefinitions(a.tools)
	if err != nil {
		return Result{}, err
	}

	for turn := 0; turn < a.maxTurns; turn++ {
		prepared, err := a.prepareMessages(ctx, req.SessionID, req.Turn, messages, tools)
		if err != nil {
			return Result{}, a.recordError(ctx, req.SessionID, req.Turn, err)
		}
		messages = prepared.messages
		stream, err := a.provider.Chat(ctx, provider.Request{
			Model:     a.model,
			Messages:  cloneMessages(prepared.requestMessages),
			Tools:     tools,
			Reasoning: cloneReasoning(a.reasoning),
		})
		if err != nil {
			return Result{}, a.recordError(ctx, req.SessionID, req.Turn, err)
		}
		consumed, err := a.consumeStream(ctx, req.SessionID, req.Turn, stream, prepared.estimatedTokens)
		closeErr := stream.Close()
		if err != nil {
			return Result{}, a.recordError(ctx, req.SessionID, req.Turn, err)
		}
		if closeErr != nil {
			return Result{}, a.recordError(ctx, req.SessionID, req.Turn, closeErr)
		}
		if len(consumed.message.Content) > 0 {
			messages = append(messages, consumed.message)
			if err := a.append(ctx, req.SessionID, func() (rollout.Event, error) {
				return rollout.AssistantMessage(req.SessionID, req.Turn, consumed.message)
			}); err != nil {
				return Result{}, err
			}
		}
		if len(consumed.toolCalls) == 0 {
			a.emit(Event{Type: EventDone, Text: consumed.text})
			return Result{Text: consumed.text, Messages: messages}, nil
		}
		for _, call := range consumed.toolCalls {
			result := a.runTool(ctx, req.SessionID, call)
			messages = append(messages, message.New(message.RoleTool, message.ToolResultBlock(call.ID, result.Content, result.IsError)))
			if err := a.append(ctx, req.SessionID, func() (rollout.Event, error) {
				return rollout.ToolResult(req.SessionID, req.Turn, call.ID, call.Name, result)
			}); err != nil {
				return Result{}, err
			}
		}
	}
	return a.finalizeWithoutTools(ctx, req.SessionID, req.Turn, messages)
}

func (a *Agent) finalizeWithoutTools(ctx context.Context, sessionID string, turn int, messages []message.Message) (Result, error) {
	a.emit(Event{
		Type:         EventActivity,
		ActivityKind: ActivityNotice,
		Status:       "running",
		Summary:      fmt.Sprintf("tool loop reached %d turns; finalizing without tools", a.maxTurns),
	})

	requestMessages := cloneMessages(messages)
	requestMessages = append(requestMessages, message.Text(message.RoleSystem, maxTurnsFinalInstruction))
	providerMessages := a.withRuntimeContext(requestMessages)
	estimated := contextmgr.EstimateRequest(providerMessages, nil, a.model)
	a.emitContextUsage(estimated, false)
	stream, err := a.provider.Chat(ctx, provider.Request{
		Model:     a.model,
		Messages:  cloneMessages(providerMessages),
		Reasoning: cloneReasoning(a.reasoning),
	})
	if err != nil {
		return Result{}, a.recordError(ctx, sessionID, turn, fmt.Errorf("%w: final no-tool request failed: %v", ErrMaxTurns, err))
	}
	consumed, err := a.consumeStream(ctx, sessionID, turn, stream, estimated)
	closeErr := stream.Close()
	if err != nil {
		return Result{}, a.recordError(ctx, sessionID, turn, fmt.Errorf("%w: final no-tool stream failed: %v", ErrMaxTurns, err))
	}
	if closeErr != nil {
		return Result{}, a.recordError(ctx, sessionID, turn, fmt.Errorf("%w: final no-tool stream close failed: %v", ErrMaxTurns, closeErr))
	}
	if len(consumed.toolCalls) > 0 {
		return Result{}, a.recordError(ctx, sessionID, turn, fmt.Errorf("%w: final no-tool response still requested %d tool call(s)", ErrMaxTurns, len(consumed.toolCalls)))
	}
	if len(consumed.message.Content) == 0 {
		return Result{}, a.recordError(ctx, sessionID, turn, fmt.Errorf("%w: final no-tool response was empty", ErrMaxTurns))
	}
	messages = append(messages, consumed.message)
	if err := a.append(ctx, sessionID, func() (rollout.Event, error) {
		return rollout.AssistantMessage(sessionID, turn, consumed.message)
	}); err != nil {
		return Result{}, err
	}
	a.emit(Event{Type: EventDone, Text: consumed.text})
	return Result{Text: consumed.text, Messages: messages}, nil
}

func cloneReasoning(cfg *reasoning.Config) *reasoning.Config {
	if cfg == nil {
		return nil
	}
	cp := *cfg
	return &cp
}

func (a *Agent) consumeStream(ctx context.Context, sessionID string, turn int, stream provider.Stream, estimatedTokens int) (streamResult, error) {
	var text strings.Builder
	var blocks []message.ContentBlock
	var calls []toolCall
	for {
		event, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return streamResult{}, err
		}
		switch event.Type {
		case provider.EventTextDelta:
			text.WriteString(event.Text)
			a.emit(Event{Type: EventDeltaText, Text: event.Text})
		case provider.EventReasoningDelta:
			a.emitThinkingActivity(reasoningSummary(event.Reasoning, event.Text), reasoningDetail(event.Reasoning, event.Text))
		case provider.EventToolCall:
			call := toolCall{
				ID:    strings.TrimSpace(event.ToolUseID),
				Name:  event.ToolName,
				Input: cloneRaw(event.Input),
			}
			if call.ID == "" {
				call.ID = rollout.NewID("tool")
			}
			calls = append(calls, call)
			blocks = append(blocks, message.ToolUseBlock(call.ID, call.Name, call.Input))
			a.emitToolActivity(call, "queued", summarizeToolInput(call.Name, call.Input), "", false)
		case provider.EventUsage:
			if event.Usage != nil {
				observeInputUsage(a.model, estimatedTokens, event.Usage.InputTokens)
				a.emitActualContextUsage(event.Usage.InputTokens)
				if err := a.append(ctx, sessionID, func() (rollout.Event, error) {
					return rollout.UsageWithDetails(sessionID, turn, usagePayload(event.Usage))
				}); err != nil {
					return streamResult{}, err
				}
			}
		case provider.EventDone:
			goto done
		case provider.EventError:
			if event.Err != nil {
				return streamResult{}, event.Err
			}
			return streamResult{}, errors.New("provider returned error event")
		default:
			return streamResult{}, fmt.Errorf("provider returned unsupported event type %q", event.Type)
		}
	}
done:
	if text.Len() > 0 {
		blocks = append([]message.ContentBlock{message.TextBlock(text.String())}, blocks...)
	}
	return streamResult{
		text:      text.String(),
		message:   message.New(message.RoleAssistant, blocks...),
		toolCalls: calls,
	}, nil
}

func usagePayload(usage *provider.Usage) rollout.UsagePayload {
	if usage == nil {
		return rollout.UsagePayload{}
	}
	return rollout.UsagePayload{
		InputTokens:      usage.InputTokens,
		OutputTokens:     usage.OutputTokens,
		ReasoningTokens:  usage.ReasoningTokens,
		CacheReadTokens:  usage.CacheReadTokens,
		CacheWriteTokens: usage.CacheWriteTokens,
	}
}

func (a *Agent) runTool(ctx context.Context, sessionID string, call toolCall) tool.Result {
	t, ok := a.tools.Get(call.Name)
	if !ok {
		result := tool.Result{Content: fmt.Sprintf("tool %q not found", call.Name), IsError: true}
		a.emitToolActivity(call, "failed", summarizeToolInput(call.Name, call.Input), summarizeToolResult(result), true)
		return result
	}
	var preview *tool.Preview
	if previewable, ok := t.(tool.PreviewableTool); ok {
		pv, err := previewable.Preview(ctx, call.Input)
		if err != nil {
			result := tool.Result{Content: fmt.Sprintf("preview %q: %v", call.Name, err), IsError: true}
			a.emitToolActivity(call, "failed", summarizeToolInput(call.Name, call.Input), summarizeToolResult(result), true)
			return result
		}
		preview = &pv
	}
	a.emitToolActivity(call, "running", summarizeToolInput(call.Name, call.Input), "", false)
	if a.permission != nil {
		approvalObserved := false
		result, err := a.permission.Ask(ctx, permission.Request{
			Tool:             call.Name,
			Args:             call.Input,
			Risk:             t.Risk(),
			Mode:             a.currentMode(),
			Preview:          preview,
			ApprovalObserver: a.permissionObserver(call.Name, &approvalObserved),
		})
		if err != nil {
			a.emitPermissionActivity(call.Name, "permission", "error", err.Error(), false)
			result := tool.Result{Content: fmt.Sprintf("permission %q: %v", call.Name, err), IsError: true}
			a.emitToolActivity(call, "failed", summarizeToolInput(call.Name, call.Input), summarizeToolResult(result), true)
			return result
		}
		if (t.Risk() == tool.RiskExec || !result.Allowed) && !(approvalObserved && result.Source == permission.SourceApprovalAgent) {
			a.emitPermissionActivity(call.Name, string(result.Source), string(result.Decision), result.Reason, result.Allowed)
		}
		if !result.Allowed {
			reason := strings.TrimSpace(result.Reason)
			if reason == "" {
				reason = string(result.Decision)
			}
			result := tool.Result{Content: fmt.Sprintf("permission denied for %q: %s", call.Name, reason), IsError: true}
			a.emitToolActivity(call, "failed", summarizeToolInput(call.Name, call.Input), summarizeToolResult(result), true)
			return result
		}
	}
	result, err := t.Execute(ctx, call.Input)
	if err != nil {
		result := tool.Result{Content: err.Error(), IsError: true}
		a.emitToolActivity(call, "failed", summarizeToolInput(call.Name, call.Input), summarizeToolResult(result), true)
		return a.limitToolResult(sessionID, call, result)
	}
	result = a.limitToolResult(sessionID, call, result)
	status := "done"
	if result.IsError {
		status = "failed"
	}
	summary := summarizeToolInput(call.Name, call.Input)
	content := summarizeToolResult(result)
	if len(result.Files) > 0 {
		summary = summarizeToolResult(result)
		content = toolResultDetail(result)
	}
	a.emitToolActivity(call, status, summary, content, result.IsError)
	return result
}

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

func (a *Agent) permissionObserver(toolName string, observed *bool) func(permission.ApprovalObservation) {
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
		a.emitPermissionActivity(toolName, string(permission.SourceApprovalAgent), decision, reason, obs.Err == nil && decision == "allow")
	}
}

func (a *Agent) currentMode() execution.Mode {
	if a.modeFunc == nil {
		return a.mode
	}
	mode, err := execution.ParseMode(string(a.modeFunc()))
	if err != nil {
		return a.mode
	}
	return mode
}

func (a *Agent) append(ctx context.Context, sessionID string, build func() (rollout.Event, error)) error {
	if a.rollout == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	event, err := build()
	if err != nil {
		return err
	}
	return a.rollout.Append(ctx, event)
}

func (a *Agent) emit(event Event) {
	if a.events != nil {
		a.events(event)
	}
}

func (a *Agent) recordError(ctx context.Context, sessionID string, turn int, err error) error {
	if err == nil {
		return nil
	}
	a.emit(Event{Type: EventError, Content: err.Error(), IsError: true, Err: err})
	if appendErr := a.append(ctx, sessionID, func() (rollout.Event, error) {
		return rollout.Error(sessionID, turn, err)
	}); appendErr != nil {
		return fmt.Errorf("record rollout error: %v; original error: %w", appendErr, err)
	}
	return err
}

func toolDefinitions(reg *tool.Registry) ([]provider.ToolDefinition, error) {
	tools := reg.All()
	defs := make([]provider.ToolDefinition, 0, len(tools))
	for _, t := range tools {
		raw, err := json.Marshal(t.Schema())
		if err != nil {
			return nil, fmt.Errorf("marshal schema for tool %q: %w", t.Name(), err)
		}
		defs = append(defs, provider.ToolDefinition{
			Name:        t.Name(),
			Description: t.Description(),
			Schema:      raw,
		})
	}
	return defs, nil
}

func cloneMessages(messages []message.Message) []message.Message {
	if messages == nil {
		return nil
	}
	out := make([]message.Message, len(messages))
	for i, msg := range messages {
		out[i] = msg.Clone()
	}
	return out
}

func cloneRaw(in json.RawMessage) json.RawMessage {
	if in == nil {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}
