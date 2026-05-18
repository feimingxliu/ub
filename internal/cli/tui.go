package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/feimingxliu/ub/internal/agent"
	"github.com/feimingxliu/ub/internal/approval"
	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/execution"
	logx "github.com/feimingxliu/ub/internal/log"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/modelinfo"
	"github.com/feimingxliu/ub/internal/permission"
	"github.com/feimingxliu/ub/internal/provider"
	"github.com/feimingxliu/ub/internal/reasoning"
	"github.com/feimingxliu/ub/internal/store"
	"github.com/feimingxliu/ub/internal/tui"
	"github.com/spf13/cobra"
)

func runTUI(cmd *cobra.Command, cfg *config.Config, resume string) (err error) {
	logger, cleanupLog, logPath, err := logx.SetupTUIFromEnv(cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := cleanupLog(); closeErr != nil && err == nil {
			err = fmt.Errorf("close tui log: %w", closeErr)
		}
	}()
	logger.Info("tui start", "log_file", logPath)
	defer logger.Info("tui stop")

	permBridge := tui.NewPermissionBridge()
	runner, err := newTUIAgentRunner(cmd, cfg, permBridge)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := runner.Close(); closeErr != nil {
			logger.Error("close tui runner", "err", closeErr)
		}
	}()

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
	err = tui.Run(cmd.Context(), tui.Options{
		Input:          cmd.InOrStdin(),
		Output:         cmd.OutOrStdout(),
		Runner:         runner,
		Permissions:    permBridge.Requests(),
		Model:          runner.model,
		Models:         runner.Models(),
		Effort:         runner.Effort(),
		Efforts:        runner.Efforts(),
		ApprovalModel:  runner.ApprovalModel(),
		ApprovalModels: runner.ApprovalModels(),
		Messages:       runner.Messages(),
		Turn:           runner.Turn(),
		ExecutionMode:  string(runner.mode),
		Cwd:            cwd,
		EventTimeout:   runner.eventTimeout,
	})
	if err != nil {
		logger.Error("tui failed", "err", err)
	}
	return err
}

type tuiAgentRunner struct {
	cmd                  *cobra.Command
	provider             provider.Provider
	providerName         string
	providerCfg          config.ProviderConfig
	model                string
	models               []string
	reasoningPref        reasoning.Config
	reasoning            *reasoning.Config
	efforts              []string
	approvalProviderName string
	approvalProviderCfg  config.ProviderConfig
	approvalModel        string
	approvalModels       []string
	approvalReasoning    reasoning.Config
	mode                 execution.Mode
	modeMu               sync.RWMutex
	eventTimeout         time.Duration
	permission           *permission.Manager
	state                *chatSessionState
	closedStore          bool
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
	model, err = selectProviderModel(cmd.Context(), providerName, providerCfg, model)
	if err != nil {
		return nil, err
	}
	p, err := provider.New(providerName, providerCfg)
	if err != nil {
		return nil, fmt.Errorf("create provider %q: %w", providerName, err)
	}
	models := providerModels(cmd.Context(), providerName, providerCfg, model)
	mainInfo := modelinfo.Resolve(providerName, providerCfg, model)
	reasoningCfg := modelinfo.RequestConfig(cfg.Reasoning, mainInfo)
	mode, err := execution.ParseMode(cfg.ExecutionMode)
	if err != nil {
		return nil, err
	}
	approvalSetup, err := newApprovalAgentSetup(cmd.Context(), cfg, providerName, model)
	if err != nil {
		return nil, err
	}
	perm, err := permission.NewManager(permission.Options{Asker: asker, ApprovalAgent: approvalSetup.Agent})
	if err != nil {
		return nil, err
	}
	return &tuiAgentRunner{
		cmd:                  cmd,
		provider:             p,
		providerName:         providerName,
		providerCfg:          providerCfg,
		model:                model,
		models:               models,
		reasoningPref:        cfg.Reasoning,
		reasoning:            reasoningCfg,
		efforts:              modelinfo.EffortOptions(mainInfo),
		approvalProviderName: approvalSetup.ProviderName,
		approvalProviderCfg:  approvalSetup.ProviderConfig,
		approvalModel:        approvalSetup.Model,
		approvalModels:       approvalSetup.Models,
		approvalReasoning:    cfg.ApprovalAgent.Reasoning,
		mode:                 mode,
		eventTimeout:         effectiveTUIEventTimeout(providerCfg.Timeout),
		permission:           perm,
	}, nil
}

func effectiveTUIEventTimeout(timeout time.Duration) time.Duration {
	// Provider/tool timeouts are enforced in their own layers. The TUI event
	// waiter must not turn normal waits for human approval or long-running
	// tools into a synthetic fatal error.
	return 0
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
		Mode:       r.currentMode(),
		ModeFunc:   r.currentMode,
		Reasoning:  cloneReasoningConfig(r.reasoning),
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
		r.refreshReasoning()
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
	r.refreshReasoning()
	return nil
}

func (r *tuiAgentRunner) SetEffort(effort string) error {
	info := modelinfo.Resolve(r.providerName, r.providerCfg, r.model)
	parsed, err := modelinfo.ValidateEffort(info, effort)
	if err != nil {
		return err
	}
	r.reasoningPref.Effort = parsed
	r.refreshReasoning()
	return nil
}

func (r *tuiAgentRunner) SetApprovalModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("approval model cannot be empty")
	}
	if r.approvalProviderName == "" {
		return fmt.Errorf("approval provider is not configured")
	}
	if !modelInList(r.approvalModels, model) {
		return fmt.Errorf("approval model %q is not available for the current approval provider", model)
	}
	agent, err := r.newApprovalAgent(model)
	if err != nil {
		return err
	}
	r.permission.SetApprovalAgent(agent)
	r.approvalModel = model
	r.approvalModels = appendModelCandidate(r.approvalModels, model)
	return nil
}

func (r *tuiAgentRunner) SetMode(mode string) error {
	parsed, err := execution.ParseMode(mode)
	if err != nil {
		return err
	}
	r.modeMu.Lock()
	defer r.modeMu.Unlock()
	r.mode = parsed
	return nil
}

func (r *tuiAgentRunner) currentMode() execution.Mode {
	r.modeMu.RLock()
	defer r.modeMu.RUnlock()
	return r.mode
}

func (r *tuiAgentRunner) Models() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.models...)
}

func (r *tuiAgentRunner) Effort() string {
	if r == nil || r.reasoning == nil || r.reasoning.Effort == "" {
		return string(reasoning.EffortNone)
	}
	return string(r.reasoning.Effort)
}

func (r *tuiAgentRunner) Efforts() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.efforts...)
}

func (r *tuiAgentRunner) refreshReasoning() {
	info := modelinfo.Resolve(r.providerName, r.providerCfg, r.model)
	r.efforts = modelinfo.EffortOptions(info)
	r.reasoning = modelinfo.RequestConfig(r.reasoningPref, info)
}

func (r *tuiAgentRunner) ApprovalModel() string {
	if r == nil {
		return ""
	}
	return r.approvalModel
}

func (r *tuiAgentRunner) ApprovalModels() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.approvalModels...)
}

func (r *tuiAgentRunner) newApprovalAgent(model string) (approval.Agent, error) {
	p, err := provider.New(r.approvalProviderName, r.approvalProviderCfg)
	if err != nil {
		return nil, fmt.Errorf("create approval provider %q: %w", r.approvalProviderName, err)
	}
	reasoningCfg := modelinfo.RequestConfig(r.approvalReasoning, modelinfo.Resolve(r.approvalProviderName, r.approvalProviderCfg, model))
	return approval.NewProviderAgentWithReasoning(p, model, reasoningCfg)
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
	case agent.EventActivity:
		return tui.Event{
			Type:         tui.EventActivity,
			ToolUseID:    event.ToolUseID,
			ToolName:     event.ToolName,
			Content:      event.Content,
			ActivityKind: string(event.ActivityKind),
			Status:       event.Status,
			Summary:      event.Summary,
			Decision:     event.Decision,
			Source:       event.Source,
			Reason:       event.Reason,
			Allowed:      event.Allowed,
			IsError:      event.IsError,
		}
	case agent.EventToolCallStart:
		return tui.Event{Type: tui.EventToolCallStart, ToolName: event.ToolName}
	case agent.EventToolCallEnd:
		return tui.Event{Type: tui.EventToolCallEnd, ToolName: event.ToolName, Content: event.Content, IsError: event.IsError}
	case agent.EventPermission:
		return tui.Event{
			Type:     tui.EventPermission,
			ToolName: event.ToolName,
			Decision: event.Decision,
			Source:   event.Source,
			Reason:   event.Reason,
			Allowed:  event.Allowed,
		}
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
