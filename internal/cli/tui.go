package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/feimingxliu/ub/internal/agent"
	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/execution"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/permission"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/store"
	"github.com/feimingxliu/ub/internal/tui"
	"github.com/spf13/cobra"
)

func runTUI(cmd *cobra.Command, cfg *config.Config, resume string) error {
	permBridge := tui.NewPermissionBridge()
	runner, err := newTUIAgentRunner(cmd, cfg, permBridge)
	if err != nil {
		return err
	}
	defer runner.Close()

	if strings.TrimSpace(resume) != "" {
		sessionID, err := resolveResumeSessionID(cmd, resume)
		if err != nil {
			return err
		}
		if _, err := runner.SwitchSession(cmd.Context(), sessionID); err != nil {
			return err
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	return tui.Run(cmd.Context(), tui.Options{
		Input:         cmd.InOrStdin(),
		Output:        cmd.OutOrStdout(),
		Runner:        runner,
		Permissions:   permBridge.Requests(),
		Model:         runner.model,
		Models:        runner.Models(),
		Messages:      runner.Messages(),
		Turn:          runner.Turn(),
		ExecutionMode: string(runner.mode),
		Cwd:           cwd,
	})
}

type tuiAgentRunner struct {
	cmd         *cobra.Command
	provider    provider.Provider
	model       string
	models      []string
	mode        execution.Mode
	permission  *permission.Manager
	state       *chatSessionState
	closedStore bool
}

func newTUIAgentRunner(cmd *cobra.Command, cfg *config.Config, asker permission.Asker) (*tuiAgentRunner, error) {
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
	models := providerModels(cmd.Context(), providerName, providerCfg, model)
	mode, err := execution.ParseMode(cfg.ExecutionMode)
	if err != nil {
		return nil, err
	}
	perm, err := permission.NewManager(permission.Options{Asker: asker})
	if err != nil {
		return nil, err
	}
	return &tuiAgentRunner{
		cmd:        cmd,
		provider:   p,
		model:      model,
		models:     models,
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

func (r *tuiAgentRunner) ListSessions(ctx context.Context) ([]tui.SessionInfo, error) {
	sessions, err := listCurrentWorkspaceSessions(ctx, 20)
	if err != nil {
		return nil, err
	}
	out := make([]tui.SessionInfo, 0, len(sessions))
	current := r.CurrentSessionID()
	for _, sess := range sessions {
		out = append(out, tui.SessionInfo{
			ID:        sess.ID,
			Title:     sess.Title,
			Model:     sess.Model,
			UpdatedAt: sess.UpdatedAt,
			Current:   sess.ID == current,
		})
	}
	return out, nil
}

func (r *tuiAgentRunner) SwitchSession(ctx context.Context, id string) (tui.SessionState, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return tui.SessionState{}, fmt.Errorf("session id is empty")
	}
	state, err := startChatRollout(r.cmd, "", r.model, chatOptions{SessionID: id})
	if err != nil {
		return tui.SessionState{}, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		_ = state.store.Close()
		return tui.SessionState{}, fmt.Errorf("get cwd: %w", err)
	}
	if state.session.Workspace != cwd {
		_ = state.store.Close()
		return tui.SessionState{}, fmt.Errorf("session %q belongs to workspace %q", id, state.session.Workspace)
	}
	if r.state != nil {
		_ = r.state.store.Close()
	}
	r.state = state
	r.closedStore = false
	if strings.TrimSpace(state.session.Model) != "" {
		r.model = state.session.Model
		r.models = appendModelCandidate(r.models, state.session.Model)
	}
	return r.sessionState(), nil
}

func (r *tuiAgentRunner) CurrentSessionID() string {
	if r == nil || r.state == nil {
		return ""
	}
	return r.state.sessionID
}

func (r *tuiAgentRunner) Messages() []tui.InitialMessage {
	if r == nil || r.state == nil {
		return nil
	}
	return messagesForTUI(r.state.history)
}

func (r *tuiAgentRunner) Turn() int {
	if r == nil || r.state == nil || r.state.nextTurn <= 1 {
		return 0
	}
	return r.state.nextTurn - 1
}

func (r *tuiAgentRunner) sessionState() tui.SessionState {
	if r == nil || r.state == nil {
		return tui.SessionState{}
	}
	return tui.SessionState{
		ID:       r.state.sessionID,
		Model:    r.model,
		Turn:     r.Turn(),
		Messages: messagesForTUI(r.state.history),
	}
}

func (r *tuiAgentRunner) SetModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("model cannot be empty")
	}
	if !modelInList(r.models, model) {
		return fmt.Errorf("model %q is not available for the current provider", model)
	}
	r.model = model
	r.models = appendModelCandidate(r.models, model)
	return nil
}

func (r *tuiAgentRunner) SetMode(mode string) error {
	parsed, err := execution.ParseMode(mode)
	if err != nil {
		return err
	}
	r.mode = parsed
	return nil
}

func (r *tuiAgentRunner) Models() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.models...)
}

func providerModels(ctx context.Context, providerName string, providerCfg config.ProviderConfig, current string) []string {
	result := checkProvider(ctx, providerName, providerCfg)
	return appendModelCandidate(result.Models, current)
}

func appendModelCandidate(models []string, model string) []string {
	model = strings.TrimSpace(model)
	seen := map[string]struct{}{}
	var out []string
	add := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	add(model)
	for _, candidate := range models {
		add(candidate)
	}
	return out
}

func modelInList(models []string, model string) bool {
	for _, candidate := range models {
		if candidate == model {
			return true
		}
	}
	return false
}

func resolveResumeSessionID(cmd *cobra.Command, resume string) (string, error) {
	resume = strings.TrimSpace(resume)
	if resume == "" {
		return "", fmt.Errorf("resume session id is empty")
	}
	if resume != "latest" {
		return resume, nil
	}
	sessions, err := listCurrentWorkspaceSessions(cmd.Context(), 1)
	if err != nil {
		return "", err
	}
	if len(sessions) == 0 {
		return "", fmt.Errorf("no sessions to resume in this workspace")
	}
	return sessions[0].ID, nil
}

func listCurrentWorkspaceSessions(ctx context.Context, limit int) ([]store.Session, error) {
	path, err := store.DefaultPath()
	if err != nil {
		return nil, fmt.Errorf("locate session store: %w", err)
	}
	st, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get cwd: %w", err)
	}
	return st.ListSessions(ctx, cwd, limit)
}

func messagesForTUI(history []message.Message) []tui.InitialMessage {
	out := make([]tui.InitialMessage, 0, len(history))
	for _, msg := range history {
		text := strings.TrimSpace(msg.Text())
		if text == "" {
			continue
		}
		out = append(out, tui.InitialMessage{
			Role: string(msg.Role),
			Text: text,
		})
	}
	return out
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
