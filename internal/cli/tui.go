package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/feimingxliu/ub/internal/agent"
	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/permission"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/tui"
	"github.com/spf13/cobra"
)

func runTUI(cmd *cobra.Command, cfg *config.Config) error {
	runner, err := newTUIAgentRunner(cmd, cfg)
	if err != nil {
		return err
	}
	defer runner.Close()

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	return tui.Run(cmd.Context(), tui.Options{
		Input:         cmd.InOrStdin(),
		Output:        cmd.OutOrStdout(),
		Runner:        runner,
		Model:         runner.model,
		ExecutionMode: string(runner.mode),
		Cwd:           cwd,
	})
}

type tuiAgentRunner struct {
	cmd         *cobra.Command
	provider    provider.Provider
	model       string
	mode        execution.Mode
	permission  *permission.Manager
	state       *chatSessionState
	closedStore bool
}

func newTUIAgentRunner(cmd *cobra.Command, cfg *config.Config) (*tuiAgentRunner, error) {
	providerName, model, err := selectChatProvider(cfg, "", "")
	if err != nil {
		return nil, err
	}
	providerCfg, ok := cfg.Providers[providerName]
	if !ok {
		return nil, fmt.Errorf("provider %q not configured; check `ub config show`", providerName)
	}
	p, err := provider.New(providerName, providerCfg)
	if err != nil {
		return nil, fmt.Errorf("create provider %q: %w", providerName, err)
	}
	mode, err := execution.ParseMode(cfg.ExecutionMode)
	if err != nil {
		return nil, err
	}
	perm, err := permission.NewManager(permission.Options{Asker: denyAsker{}})
	if err != nil {
		return nil, err
	}
	return &tuiAgentRunner{
		cmd:        cmd,
		provider:   p,
		model:      model,
		mode:       mode,
		permission: perm,
	}, nil
}

func (r *tuiAgentRunner) Run(ctx context.Context, prompt string, events chan<- tui.Event) error {
	if r.state == nil {
		state, err := startChatRollout(r.cmd, prompt, r.model, chatOptions{})
		if err != nil {
			return err
		}
		r.state = state
	}
	reg, err := localToolRegistry()
	if err != nil {
		return err
	}
	a, err := agent.New(agent.Options{
		Provider:   r.provider,
		Tools:      reg,
		Permission: r.permission,
		Rollout:    r.state.rollout,
		Model:      r.model,
		Mode:       r.mode,
		Events: func(event agent.Event) {
			sendTUIEvent(ctx, events, convertAgentEvent(event))
		},
	})
	if err != nil {
		return err
	}
	result, err := a.Run(ctx, agent.Request{
		SessionID: r.state.sessionID,
		Turn:      r.state.nextTurn,
		History:   r.state.history,
		Prompt:    prompt,
	})
	if err != nil {
		_ = finishChatSession(r.cmd, r.state, prompt, r.model)
		return err
	}
	r.state.history = result.Messages
	r.state.nextTurn++
	return finishChatSession(r.cmd, r.state, prompt, r.model)
}

func (r *tuiAgentRunner) Close() error {
	if r == nil || r.state == nil || r.closedStore {
		return nil
	}
	r.closedStore = true
	return r.state.store.Close()
}

type denyAsker struct{}

func (denyAsker) Ask(context.Context, permission.Request) (permission.Decision, error) {
	return permission.DecisionDeny, nil
}

func convertAgentEvent(event agent.Event) tui.Event {
	switch event.Type {
	case agent.EventDeltaText:
		return tui.Event{Type: tui.EventDeltaText, Text: event.Text}
	case agent.EventToolCallStart:
		return tui.Event{Type: tui.EventToolCallStart, ToolName: event.ToolName}
	case agent.EventToolCallEnd:
		return tui.Event{Type: tui.EventToolCallEnd, ToolName: event.ToolName, Content: event.Content, IsError: event.IsError}
	case agent.EventDone:
		return tui.Event{Type: tui.EventDone, Text: event.Text}
	case agent.EventError:
		return tui.Event{Type: tui.EventError, Content: event.Content, IsError: true, Err: event.Err}
	default:
		return tui.Event{Type: tui.EventError, Content: fmt.Sprintf("unknown agent event %q", event.Type), IsError: true}
	}
}

func sendTUIEvent(ctx context.Context, events chan<- tui.Event, event tui.Event) {
	select {
	case events <- event:
	case <-ctx.Done():
	}
}
