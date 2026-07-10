package cli

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/feimingxliu/ub/internal/app/ub/agent"
	"github.com/feimingxliu/ub/internal/app/ub/tui"
	"github.com/feimingxliu/ub/internal/pkg/runtime/hook"
	goaltool "github.com/feimingxliu/ub/internal/pkg/tool/goal"
)

func (r *tuiAgentRunner) Run(ctx context.Context, prompt string, events chan<- tui.Event) error {
	if r.state == nil {
		state, err := startChatRollout(r.cmd, prompt, r.providerName, r.model, chatOptions{})
		if err != nil {
			return err
		}
		r.state = state
	}
	// runOnce performs a single agent turn. It returns (done, error) where
	// done=true means the loop should stop (no active goal, or terminal goal).
	// On error, finishChatSession is NOT called here — the caller handles it.
	runOnce := func(prompt string) (done bool, err error) {
		a, err := r.newAgent(ctx, events)
		if err != nil {
			return true, err
		}
		result, err := a.Run(ctx, agent.Request{
			SessionID:      r.state.sessionID,
			Turn:           r.state.nextTurn,
			History:        r.state.history,
			ContextHistory: r.state.contextHistory,
			Prompt:         prompt,
		})
		if err != nil {
			return true, err
		}
		r.state.history = result.Messages
		r.state.contextHistory = result.ContextMessages
		r.state.nextTurn++
		return false, nil
	}
	// First turn: run the user's prompt.
	done, err := runOnce(prompt)
	if err != nil {
		_ = finishChatSession(r.cmd, r.state, prompt, r.providerName, r.model)
		return err
	}
	if done {
		return finishChatSession(r.cmd, r.state, prompt, r.providerName, r.model)
	}
	// Goal auto-continuation loop: after each turn, if the session has an
	// active (non-terminal) goal, inject a continuation prompt and run again.
	goalContinueCount := 0
	for {
		g, loadErr := goaltool.Load(r.state.sessionID)
		if loadErr != nil {
			slog.Warn("goal load error", "session", r.state.sessionID, "err", loadErr)
			break
		}
		if g == nil || goaltool.IsTerminal(g.Status) {
			if g != nil {
				// Emit a final status notice so the TUI can show the terminal
				// state (complete / blocked / budget_limited) in the status bar.
				finalEvt := tui.Event{
					Type:         tui.EventActivity,
					ActivityKind: "notice",
					Notice:       "goal_status",
					Status:       string(g.Status),
					Summary:      fmt.Sprintf("Goal %s: %s", g.Status, truncateGoalObjective(g.Objective, 60)),
					Content:      g.Objective,
				}
				sendTUIEvent(ctx, events, finalEvt)
			}
			break
		}
		// Record usage for the completed turn.
		if usageErr := goaltool.RecordUsage(r.state.sessionID, 0); usageErr != nil {
			slog.Warn("goal record usage error", "err", usageErr)
		}
		// Re-check after usage recording (budget may have been hit).
		g, _ = goaltool.Load(r.state.sessionID)
		if g == nil || goaltool.IsTerminal(g.Status) {
			break
		}
		goalContinueCount++
		// Emit a continuation notice event so the TUI can show the status.
		evt := tui.Event{
			Type:         tui.EventActivity,
			ActivityKind: "notice",
			Notice:       "goal_inject",
			Status:       "running",
			Summary:      fmt.Sprintf("Goal continuing (%d): %s", goalContinueCount, truncateGoalObjective(g.Objective, 60)),
			Content:      g.Objective,
		}
		sendTUIEvent(ctx, events, evt)
		contPrompt := agent.GoalContinuationPrompt(g)
		done, err = runOnce(contPrompt)
		if err != nil {
			break
		}
		if done {
			break
		}
	}
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

// Inject sends user guidance text into the currently running agent loop.
// The text appears as a user message in the next loop iteration, guiding the
// model without starting a new turn. It reports false (and drops the text)
// when no run is active or the inject channel is full, so the caller can keep
// the UI consistent with what actually reaches the agent.
func (r *tuiAgentRunner) Inject(text string) bool {
	if r == nil || r.injectCh == nil {
		return false
	}
	select {
	case r.injectCh <- text:
		return true
	default:
		return false
	}
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
	contextWindow := newContextWindowResolver(r.providerName, r.providerCfg, r.model, maxContext, r.provider)
	runtime := agentRuntimeContext(r.tools.Workspace)
	hooksRunner := hook.New(r.cfg.Hooks)
	fileHistory, err := newFileHistoryManager(ctx, r.tools.Workspace, r.state.sessionID, r.state.rollout)
	if err != nil {
		return nil, err
	}
	// injectCh is built once on the runner and reused across runs; the agent
	// loop drains it between iterations and flushes the remainder at Run end,
	// so it is empty at the start of each run.
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
		ContextWindow:       contextWindow,
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
		Inject:              r.injectCh,
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

func truncateGoalObjective(objective string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(objective))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes]) + "..."
}
