package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/app/ub/agent"
	"github.com/feimingxliu/ub/internal/app/ub/tui"
	"github.com/feimingxliu/ub/internal/pkg/runtime/hook"
)

func (r *tuiAgentRunner) Run(ctx context.Context, prompt string, events chan<- tui.Event) error {
	if r.state == nil {
		state, err := startChatRollout(r.cmd, prompt, r.providerName, r.model, chatOptions{})
		if err != nil {
			return err
		}
		r.state = state
	}
	a, err := r.newAgent(ctx, events)
	if err != nil {
		return err
	}
	result, err := a.Run(ctx, agent.Request{
		SessionID:      r.state.sessionID,
		Turn:           r.state.nextTurn,
		History:        r.state.history,
		ContextHistory: r.state.contextHistory,
		Prompt:         prompt,
	})
	if err != nil {
		_ = finishChatSession(r.cmd, r.state, prompt, r.providerName, r.model)
		return err
	}
	r.state.history = result.Messages
	r.state.contextHistory = result.ContextMessages
	r.state.nextTurn++
	return finishChatSession(r.cmd, r.state, prompt, r.providerName, r.model)
}

func (r *tuiAgentRunner) Compact(ctx context.Context, events chan<- tui.Event) error {
	if r.state == nil {
		return fmt.Errorf("compact requires an active session")
	}
	a, err := r.newAgent(ctx, events)
	if err != nil {
		return err
	}
	result, err := a.Compact(ctx, agent.CompactRequest{
		SessionID: r.state.sessionID,
		Turn:      r.state.nextTurn,
		History:   r.state.history,
	})
	if err != nil {
		_ = finishChatSession(r.cmd, r.state, "", r.providerName, r.model)
		return err
	}
	if !result.Noop {
		r.state.contextHistory = result.Messages
	}
	return finishChatSession(r.cmd, r.state, "", r.providerName, r.model)
}

func (r *tuiAgentRunner) ListWorkspaceFiles(ctx context.Context, query string, limit int) ([]string, error) {
	if r == nil || r.tools == nil || strings.TrimSpace(r.tools.Workspace) == "" {
		return nil, fmt.Errorf("workspace is unavailable")
	}
	return listWorkspaceFiles(ctx, r.tools.Workspace, query, limit)
}

func (r *tuiAgentRunner) newAgent(ctx context.Context, events chan<- tui.Event) (*agent.Agent, error) {
	resolvedReasoning := cloneReasoningConfig(r.reasoning)
	maxContext := r.currentModelInfo().MaxContextTokens
	runtime := agentRuntimeContext(r.tools.Workspace)
	hooksRunner := hook.New(r.cfg.Hooks)
	fileHistory, err := newFileHistoryManager(ctx, r.tools.Workspace, r.state.sessionID, r.state.rollout)
	if err != nil {
		return nil, err
	}
	var parentEvents agent.EventSink
	factory := agent.NewFactory(agent.Options{
		Provider:            r.provider,
		Tools:               r.tools.Registry,
		Permission:          r.permission,
		Rollout:             r.state.rollout,
		Model:               r.model,
		Mode:                r.currentMode(),
		ModeFunc:            r.currentMode,
		PlanMode:            r.planMode,
		MaxTurns:            r.maxTurns,
		LimitAsker:          r.limitAsker,
		Asker:               r.asker,
		Reasoning:           resolvedReasoning,
		MaxContextTokens:    maxContext,
		SummaryProvider:     r.summaryProvider,
		SummaryModel:        r.summaryModel,
		AutoMemoryProvider:  r.autoMemoryProvider,
		AutoMemoryModel:     r.autoMemoryModel,
		Context:             r.contextCfg,
		Prompt:              r.cfg.Prompt,
		Runtime:             runtime,
		Hooks:               hooksRunner,
		WorkspaceRoot:       r.tools.Workspace,
		MemoryMaxChars:      r.cfg.Memory.MaxChars,
		Memory:              r.cfg.Memory,
		MemoryAutoScheduler: r.memoryAutoScheduler,
		FileHistory:         fileHistory,
		BackgroundEvents:    r.backgroundEventSink(ctx),
	})
	subRunner := &cliSubagentRunner{
		factory:          factory,
		provider:         r.provider,
		tools:            r.tools.Registry,
		permission:       r.permission,
		model:            r.model,
		mode:             r.currentMode(),
		modeFunc:         r.currentMode,
		reasoningCfg:     cloneReasoningConfig(r.reasoning),
		maxContextTokens: maxContext,
		contextCfg:       r.contextCfg,
		promptCfg:        r.cfg.Prompt,
		runtime:          runtime,
		hooks:            hooksRunner,
		defaultMaxTurns:  r.maxTurns,
		workspaceRoot:    r.tools.Workspace,
		memoryMaxChars:   r.cfg.Memory.MaxChars,
		fileHistory:      fileHistory,
		rollout:          r.state.rollout,
		events: func(event agent.Event) {
			if parentEvents != nil {
				parentEvents(event)
			}
		},
	}
	parentEvents = func(event agent.Event) {
		sendTUIEvent(ctx, events, convertAgentEvent(event))
	}
	a, err := factory.New(func(opts *agent.Options) {
		opts.SubagentRunner = subRunner
		opts.Events = parentEvents
	})
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (r *tuiAgentRunner) backgroundEventSink(ctx context.Context) agent.EventSink {
	if r == nil || r.backgroundEvents == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if r.cmd != nil && r.cmd.Context() != nil {
		ctx = r.cmd.Context()
	}
	return func(event agent.Event) {
		sendTUIEvent(ctx, r.backgroundEvents, convertAgentEvent(event))
	}
}

func (r *tuiAgentRunner) Close() error {
	if r == nil {
		return nil
	}
	var err error
	if r.tools != nil {
		if closeErr := r.tools.Close(); closeErr != nil {
			err = closeErr
		}
		r.tools = nil
	}
	if r.state == nil || r.closedStore {
		return err
	}
	r.closedStore = true
	if closeErr := r.state.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	return err
}
