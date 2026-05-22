package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	"github.com/feimingxliu/ub/internal/rollout"
	"github.com/feimingxliu/ub/internal/store"
	"github.com/feimingxliu/ub/internal/tui"
	"github.com/spf13/cobra"
)

const resumeSelectSentinel = "latest"

func runTUI(cmd *cobra.Command, cfg *config.Config, resume, providerFlag, modelFlag string) (err error) {
	logger, cleanupLog, logPath, err := logx.SetupTUIFromEnvWithRotation(cmd.ErrOrStderr(), logRotationOptions(cfg))
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
	if strings.TrimSpace(resume) == "" {
		runStartupMaintenance(cmd, cfg)
	}

	permBridge := tui.NewPermissionBridge()
	limitBridge := tui.NewLimitBridge()
	runner, err := newTUIAgentRunner(cmd, cfg, permBridge, providerFlag, modelFlag)
	if err != nil {
		return err
	}
	runner.limitAsker = limitBridge
	defer func() {
		if closeErr := runner.Close(); closeErr != nil {
			logger.Error("close tui runner", "err", closeErr)
		}
	}()

	selectSessionOnStart := shouldSelectSessionOnStart(resume)
	if !selectSessionOnStart && strings.TrimSpace(resume) != "" {
		sessionID, err := resolveResumeSessionID(resume)
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
		Limits:         limitBridge.Requests(),
		Provider:       runner.Provider(),
		Providers:      runner.Providers(),
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
		SelectSession:  selectSessionOnStart,
	})
	if err != nil {
		logger.Error("tui failed", "err", err)
	}
	return err
}

// shouldSelectSessionOnStart returns true only when the user explicitly
// asked for the session picker via the bare `--resume` flag. A plain `ub`
// invocation never opens the picker, even if the workspace has history;
// that interrupts the new-session flow people expect when they just want
// to start typing.
func shouldSelectSessionOnStart(resume string) bool {
	return strings.TrimSpace(resume) == resumeSelectSentinel
}

type tuiAgentRunner struct {
	cmd                  *cobra.Command
	cfg                  *config.Config
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
	summaryProvider      provider.Provider
	summaryModel         string
	summaryUsesCurrent   bool
	contextCfg           config.ContextConfig
	tools                *toolRuntime
	mode                 execution.Mode
	modeMu               sync.RWMutex
	eventTimeout         time.Duration
	permission           *permission.Manager
	state                *chatSessionState
	closedStore          bool
	maxTurns             int
	limitAsker           agent.LimitAsker

	// cachedMessages holds the reconstructed InitialMessages for the loaded
	// session. Populated lazily by Messages() so we only scan the rollout once
	// per session-load instead of on every sessionState() call.
	cachedMessages        []tui.InitialMessage
	cachedMessagesSession string
}

func newTUIAgentRunner(cmd *cobra.Command, cfg *config.Config, asker permission.Asker, providerFlag, modelFlag string) (*tuiAgentRunner, error) {
	providerName, model, err := selectChatProvider(cfg, providerFlag, modelFlag)
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
	summarySetup, err := newSummarySetup(cmd.Context(), cfg, providerName, providerCfg, model)
	if err != nil {
		return nil, err
	}
	perm, err := permission.NewManager(permission.Options{Asker: asker, ApprovalAgent: approvalSetup.Agent})
	if err != nil {
		return nil, err
	}
	tools, err := newToolRuntime(cmd.Context(), cfg)
	if err != nil {
		return nil, err
	}
	writeToolWarnings(cmd.ErrOrStderr(), tools.Warnings)
	return &tuiAgentRunner{
		cmd:                  cmd,
		cfg:                  cfg,
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
		summaryProvider:      summarySetup.Provider,
		summaryModel:         summarySetup.Model,
		summaryUsesCurrent:   summarySetup.UsesCurrentModel,
		contextCfg:           cfg.Context,
		tools:                tools,
		mode:                 mode,
		eventTimeout:         effectiveTUIEventTimeout(providerCfg.Timeout),
		permission:           perm,
		maxTurns:             cfg.MaxTurns,
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
	a, err := r.newAgent(ctx, events)
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
		_ = finishChatSession(r.cmd, r.state, "", r.model)
		return err
	}
	if !result.Noop {
		r.state.history = result.Messages
	}
	return finishChatSession(r.cmd, r.state, "", r.model)
}

func (r *tuiAgentRunner) RunShell(ctx context.Context, command string, events chan<- tui.Event) error {
	if r == nil || r.tools == nil || r.tools.Registry == nil {
		return fmt.Errorf("shell execution is unavailable")
	}
	bash, ok := r.tools.Registry.Get("bash")
	if !ok {
		return fmt.Errorf("bash tool is unavailable")
	}
	raw, err := json.Marshal(map[string]any{
		"command": command,
		"cwd":     ".",
	})
	if err != nil {
		return err
	}
	result, execErr := bash.Execute(ctx, raw)
	content := strings.TrimRight(result.Content, "\n")
	isError := result.IsError
	if execErr != nil {
		content = execErr.Error()
		isError = true
	}
	sendTUIEvent(ctx, events, tui.Event{
		Type:    tui.EventShellOutput,
		Content: formatShellOutput(content, isError),
		IsError: isError,
	})
	sendTUIEvent(ctx, events, tui.Event{Type: tui.EventDone})
	return nil
}

func formatShellOutput(content string, isError bool) string {
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return "(no output)"
	}
	parsed, ok := parseShellToolOutput(content)
	if !ok {
		return content
	}
	var parts []string
	if strings.TrimSpace(parsed.stdout) != "" {
		parts = append(parts, strings.TrimRight(parsed.stdout, "\n"))
	}
	if strings.TrimSpace(parsed.stderr) != "" {
		stderr := strings.TrimRight(parsed.stderr, "\n")
		if len(parts) > 0 {
			parts = append(parts, "--- stderr ---\n"+stderr)
		} else {
			parts = append(parts, stderr)
		}
	}
	if strings.TrimSpace(parsed.errorLine) != "" {
		parts = append(parts, "error: "+parsed.errorLine)
	}
	if isError && parsed.exitCode != "" && parsed.exitCode != "0" {
		parts = append(parts, "exit code: "+parsed.exitCode)
	}
	if len(parts) == 0 {
		if isError && parsed.exitCode != "" && parsed.exitCode != "0" {
			return "exit code: " + parsed.exitCode
		}
		return "(no output)"
	}
	return strings.Join(parts, "\n")
}

type shellToolOutput struct {
	exitCode  string
	errorLine string
	stdout    string
	stderr    string
}

func parseShellToolOutput(content string) (shellToolOutput, bool) {
	stdoutMarker := "\n--- stdout ---\n"
	stderrMarker := "\n--- stderr ---"
	stdoutStart := strings.Index(content, stdoutMarker)
	if stdoutStart < 0 {
		return shellToolOutput{}, false
	}
	stderrStart := strings.Index(content[stdoutStart+len(stdoutMarker):], stderrMarker)
	if stderrStart < 0 {
		return shellToolOutput{}, false
	}
	stderrStart += stdoutStart + len(stdoutMarker)
	header := content[:stdoutStart]
	stdout := content[stdoutStart+len(stdoutMarker) : stderrStart]
	stderr := strings.TrimPrefix(content[stderrStart+len(stderrMarker):], "\n")
	parsed := shellToolOutput{
		exitCode:  shellHeaderValue(header, "exit_code"),
		errorLine: shellHeaderValue(header, "error"),
		stdout:    stdout,
		stderr:    stderr,
	}
	return parsed, true
}

func shellHeaderValue(header, key string) string {
	prefix := key + "="
	for _, line := range strings.Split(header, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func (r *tuiAgentRunner) ListWorkspaceFiles(ctx context.Context, query string, limit int) ([]string, error) {
	if r == nil || r.tools == nil || strings.TrimSpace(r.tools.Workspace) == "" {
		return nil, fmt.Errorf("workspace is unavailable")
	}
	return listWorkspaceFiles(ctx, r.tools.Workspace, query, limit)
}

func (r *tuiAgentRunner) newAgent(ctx context.Context, events chan<- tui.Event) (*agent.Agent, error) {
	a, err := agent.New(agent.Options{
		Provider:         r.provider,
		Tools:            r.tools.Registry,
		Permission:       r.permission,
		Rollout:          r.state.rollout,
		Model:            r.model,
		Mode:             r.currentMode(),
		ModeFunc:         r.currentMode,
		MaxTurns:         r.maxTurns,
		LimitAsker:       r.limitAsker,
		Reasoning:        cloneReasoningConfig(r.reasoning),
		MaxContextTokens: modelinfo.Resolve(r.providerName, r.providerCfg, r.model).MaxContextTokens,
		SummaryProvider:  r.summaryProvider,
		SummaryModel:     r.summaryModel,
		Context:          r.contextCfg,
		Runtime:          agentRuntimeContext(r.tools.Workspace),
		Events: func(event agent.Event) {
			sendTUIEvent(ctx, events, convertAgentEvent(event))
		},
	})
	if err != nil {
		return nil, err
	}
	return a, nil
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
	if closeErr := r.state.store.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	return err
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

func (r *tuiAgentRunner) NewSession(ctx context.Context) (tui.SessionState, error) {
	state, err := startChatRollout(r.cmd, "", r.model, chatOptions{})
	if err != nil {
		return tui.SessionState{}, err
	}
	state.session.Title = ""
	if err := state.store.UpdateSession(ctx, state.session); err != nil {
		_ = state.store.Close()
		return tui.SessionState{}, err
	}
	if r.state != nil {
		_ = r.state.store.Close()
	}
	r.state = state
	r.closedStore = false
	r.invalidateMessageCache()
	return r.sessionState(), nil
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
	r.invalidateMessageCache()
	if strings.TrimSpace(state.session.Model) != "" {
		r.model = state.session.Model
		r.models = appendModelCandidate(r.models, state.session.Model)
		r.refreshReasoning()
	}
	if state.mode != "" {
		r.modeMu.Lock()
		r.mode = state.mode
		r.modeMu.Unlock()
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
	sessionID := r.state.sessionID
	if sessionID != "" && r.cachedMessagesSession == sessionID && r.cachedMessages != nil {
		return r.cachedMessages
	}
	var messages []tui.InitialMessage
	if msgs, err := r.messagesForCurrentSession(); err == nil {
		messages = msgs
	} else {
		messages = messagesForTUI(r.state.history)
	}
	r.cachedMessages = messages
	r.cachedMessagesSession = sessionID
	return messages
}

func (r *tuiAgentRunner) invalidateMessageCache() {
	if r == nil {
		return
	}
	r.cachedMessages = nil
	r.cachedMessagesSession = ""
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
		Messages: r.Messages(),
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
	if r.summaryUsesCurrent {
		r.summaryModel = model
	}
	r.refreshReasoning()
	return nil
}

func (r *tuiAgentRunner) SetProvider(providerName, model string) (tui.ProviderSelection, error) {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return tui.ProviderSelection{}, fmt.Errorf("provider cannot be empty")
	}
	if r == nil || r.cfg == nil {
		return tui.ProviderSelection{}, fmt.Errorf("provider switching is unavailable")
	}
	providerCfg, ok := r.cfg.Providers[providerName]
	if !ok {
		return tui.ProviderSelection{}, fmt.Errorf("provider %q not configured; check `ub config show`", providerName)
	}
	selectedModel, err := r.modelForProviderSwitch(providerName, providerCfg, model)
	if err != nil {
		return tui.ProviderSelection{}, err
	}
	p, err := provider.New(providerName, providerCfg)
	if err != nil {
		return tui.ProviderSelection{}, fmt.Errorf("create provider %q: %w", providerName, err)
	}
	models := providerModels(r.cmd.Context(), providerName, providerCfg, selectedModel)
	info := modelinfo.Resolve(providerName, providerCfg, selectedModel)
	summarySetup, err := newSummarySetup(r.cmd.Context(), r.cfg, providerName, providerCfg, selectedModel)
	if err != nil {
		return tui.ProviderSelection{}, err
	}
	approvalSetup, err := newApprovalAgentSetup(r.cmd.Context(), r.cfg, providerName, selectedModel)
	if err != nil {
		return tui.ProviderSelection{}, err
	}
	r.provider = p
	r.providerName = providerName
	r.providerCfg = providerCfg
	r.model = selectedModel
	r.models = models
	r.efforts = modelinfo.EffortOptions(info)
	r.summaryProvider = summarySetup.Provider
	r.summaryModel = summarySetup.Model
	r.summaryUsesCurrent = summarySetup.UsesCurrentModel
	r.approvalProviderName = approvalSetup.ProviderName
	r.approvalProviderCfg = approvalSetup.ProviderConfig
	r.approvalModel = approvalSetup.Model
	r.approvalModels = approvalSetup.Models
	r.approvalReasoning = r.cfg.ApprovalAgent.Reasoning
	if r.permission != nil {
		r.permission.SetApprovalAgent(approvalSetup.Agent)
	}
	r.refreshReasoning()
	return tui.ProviderSelection{
		Provider:  r.providerName,
		Providers: r.Providers(),
		Model:     r.model,
		Models:    r.Models(),
		Effort:    r.Effort(),
		Efforts:   r.Efforts(),
	}, nil
}

func (r *tuiAgentRunner) modelForProviderSwitch(providerName string, providerCfg config.ProviderConfig, model string) (string, error) {
	model = strings.TrimSpace(model)
	if model != "" {
		return model, nil
	}
	if r != nil && r.cfg != nil && strings.TrimSpace(r.cfg.DefaultProvider) == providerName && strings.TrimSpace(r.cfg.DefaultModel) != "" {
		return strings.TrimSpace(r.cfg.DefaultModel), nil
	}
	return selectProviderModel(r.cmd.Context(), providerName, providerCfg, "")
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
	if r != nil && r.state != nil && r.state.rollout != nil {
		event, err := rollout.ModeSwitch(r.state.sessionID, r.state.nextTurn, string(parsed))
		if err != nil {
			return err
		}
		if err := r.state.rollout.Append(r.cmd.Context(), event); err != nil {
			return err
		}
		r.state.session.UpdatedAt = time.Now().UTC()
		if err := r.state.store.UpdateSession(r.cmd.Context(), r.state.session); err != nil {
			return err
		}
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

func (r *tuiAgentRunner) Provider() string {
	if r == nil {
		return ""
	}
	return r.providerName
}

func (r *tuiAgentRunner) Providers() []string {
	if r == nil || r.cfg == nil {
		return nil
	}
	var out []string
	for _, name := range sortedProviderNames(r.cfg.Providers) {
		if strings.TrimSpace(r.cfg.Providers[name].Type) == "" {
			continue
		}
		out = append(out, name)
	}
	return out
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

func resolveResumeSessionID(resume string) (string, error) {
	resume = strings.TrimSpace(resume)
	if resume == "" {
		return "", fmt.Errorf("resume session id is empty")
	}
	if resume == resumeSelectSentinel {
		return "", fmt.Errorf("resume session id is empty")
	}
	return resume, nil
}

type fileCandidate struct {
	path  string
	score int
}

func listWorkspaceFiles(ctx context.Context, root, query string, limit int) ([]string, error) {
	root = filepath.Clean(root)
	if limit <= 0 {
		limit = 50
	}
	query = strings.ToLower(strings.TrimSpace(query))
	var candidates []fileCandidate
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if path == root {
			return nil
		}
		if entry.IsDir() {
			if excludedFileMentionDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		score, ok := fileMentionMatchScore(rel, query)
		if !ok {
			return nil
		}
		candidates = append(candidates, fileCandidate{path: rel, score: score})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score < candidates[j].score
		}
		if len(candidates[i].path) != len(candidates[j].path) {
			return len(candidates[i].path) < len(candidates[j].path)
		}
		return candidates[i].path < candidates[j].path
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	out := make([]string, len(candidates))
	for i, candidate := range candidates {
		out[i] = candidate.path
	}
	return out, nil
}

func excludedFileMentionDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "vendor", "dist", "build":
		return true
	default:
		return false
	}
}

func fileMentionMatchScore(path, query string) (int, bool) {
	if query == "" {
		return 0, true
	}
	path = strings.ToLower(path)
	base := strings.ToLower(filepath.Base(path))
	switch {
	case strings.HasPrefix(path, query):
		return 0, true
	case strings.HasPrefix(base, query):
		return 1, true
	case strings.Contains(path, query):
		return 2, true
	default:
		return 0, false
	}
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

func (r *tuiAgentRunner) messagesForCurrentSession() ([]tui.InitialMessage, error) {
	if r == nil || r.state == nil || r.state.rollout == nil || strings.TrimSpace(r.state.sessionID) == "" {
		return nil, fmt.Errorf("current session rollout is unavailable")
	}
	ctx := context.Background()
	if r.cmd != nil && r.cmd.Context() != nil {
		ctx = r.cmd.Context()
	}
	return messagesForTUIFromRollout(ctx, r.state.rollout, r.state.sessionID)
}

func messagesForTUIFromRollout(ctx context.Context, reader rollout.Reader, sessionID string) ([]tui.InitialMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if reader == nil {
		return nil, fmt.Errorf("rollout reader is nil")
	}
	var out []tui.InitialMessage
	toolUses := map[string]message.ContentBlock{}
	if err := reader.ForEach(ctx, sessionID, func(event rollout.Event) error {
		if activity, ok, err := rollout.ActivityFromEvent(event); err != nil {
			return err
		} else if ok {
			out = append(out, activityMessageForTUI(activity, event.Turn))
			return nil
		}

		msg, ok, err := rollout.MessageFromEvent(event)
		if err != nil {
			return err
		}
		if event.Type == rollout.TypeSummary {
			if ok {
				toolUses = map[string]message.ContentBlock{}
				out = appendMessagesForTUI(nil, toolUses, msg, event.Turn)
			}
			return nil
		}
		if ok {
			out = appendMessagesForTUI(out, toolUses, msg, event.Turn)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func messagesForTUI(history []message.Message) []tui.InitialMessage {
	out := make([]tui.InitialMessage, 0, len(history))
	toolUses := map[string]message.ContentBlock{}
	// In-memory history has no per-message turn; approximate by counting user
	// messages. Resume normally uses the rollout-backed path which carries
	// the real Turn; this is only the fallback when the rollout is missing.
	turn := 0
	for _, msg := range history {
		if msg.Role == message.RoleUser {
			turn++
		}
		out = appendMessagesForTUI(out, toolUses, msg, turn)
	}
	return out
}

func appendMessagesForTUI(out []tui.InitialMessage, toolUses map[string]message.ContentBlock, msg message.Message, turn int) []tui.InitialMessage {
	text := strings.TrimSpace(msg.Text())
	if text != "" {
		out = append(out, tui.InitialMessage{
			Role: string(msg.Role),
			Turn: turn,
			Text: text,
		})
	}
	for _, block := range msg.Content {
		switch block.Type {
		case message.BlockToolUse:
			if strings.TrimSpace(block.ToolUseID) == "" {
				continue
			}
			toolUses[block.ToolUseID] = block
			out = append(out, tui.InitialMessage{
				Turn:         turn,
				ActivityKind: "tool",
				ToolUseID:    block.ToolUseID,
				ToolName:     block.ToolName,
				Status:       "queued",
				Summary:      agent.SummarizeToolInput(block.ToolName, block.Input),
			})
		case message.BlockToolResult:
			if strings.TrimSpace(block.ToolUseID) == "" {
				continue
			}
			toolUse := toolUses[block.ToolUseID]
			status := "done"
			if block.IsError {
				status = "failed"
			}
			toolName := fallbackString(toolUse.ToolName, "tool")
			out = append(out, tui.InitialMessage{
				Turn:         turn,
				ActivityKind: "tool",
				ToolUseID:    block.ToolUseID,
				ToolName:     toolName,
				Status:       status,
				Summary:      agent.SummarizeToolInput(toolName, toolUse.Input),
				Content:      block.Output,
				IsError:      block.IsError,
			})
		}
	}
	return out
}

func activityMessageForTUI(activity rollout.ActivityPayload, turn int) tui.InitialMessage {
	return tui.InitialMessage{
		Turn:         turn,
		ActivityKind: activity.ActivityKind,
		ToolUseID:    activity.ToolUseID,
		ToolName:     activity.ToolName,
		Status:       activity.Status,
		Summary:      activity.Summary,
		Content:      activity.Content,
		Decision:     activity.Decision,
		Source:       activity.Source,
		Reason:       activity.Reason,
		Allowed:      activity.Allowed,
		IsError:      activity.IsError,
	}
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
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
	case agent.EventContext:
		return tui.Event{
			Type:              tui.EventContext,
			ContextUsedTokens: event.ContextUsedTokens,
			ContextMaxTokens:  event.ContextMaxTokens,
			ContextRatio:      event.ContextRatio,
			ContextReset:      event.ContextReset,
			ContextKind:       event.ContextKind,
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
