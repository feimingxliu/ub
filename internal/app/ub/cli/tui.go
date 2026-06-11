package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/feimingxliu/ub/internal/app/ub/agent"
	"github.com/feimingxliu/ub/internal/app/ub/tui"
	"github.com/feimingxliu/ub/internal/pkg/core/config"
	"github.com/feimingxliu/ub/internal/pkg/core/execution"
	"github.com/feimingxliu/ub/internal/pkg/core/message"
	"github.com/feimingxliu/ub/internal/pkg/core/reasoning"
	"github.com/feimingxliu/ub/internal/pkg/llm/modelinfo"
	"github.com/feimingxliu/ub/internal/pkg/llm/provider"
	"github.com/feimingxliu/ub/internal/pkg/runtime/approval"
	"github.com/feimingxliu/ub/internal/pkg/runtime/hook"
	logx "github.com/feimingxliu/ub/internal/pkg/runtime/log"
	"github.com/feimingxliu/ub/internal/pkg/runtime/permission"
	"github.com/feimingxliu/ub/internal/pkg/tool"
	mcptool "github.com/feimingxliu/ub/internal/pkg/tool/mcp"
	"github.com/feimingxliu/ub/internal/pkg/workspace/rollout"
	"github.com/feimingxliu/ub/internal/pkg/workspace/store"
	"github.com/spf13/cobra"
)

const resumeSelectSentinel = "latest"

const sideQuestionSystemPrompt = `<side_question>
You are answering an in-memory BTW side chat inside ub.
- Answer only the BTW side-chat turn. Do not take over or continue the main task.
- No tools are available in this request. Do not request tools, read files, execute commands, search, or inspect external state.
- Never emit native tool calls, tool-call JSON, XML tool tags, <tool_use>, <function=...>, or similar markup.
- Use only the provided main conversation context, BTW side-chat history, and your general knowledge.
- If the question requires fresh workspace inspection, command output, web search, or other tools, say that it needs to be asked as a normal user turn.
- Keep the answer concise and directly useful.
</side_question>`

const sideQuestionToolAttemptMessage = "btw cannot use tools in side chat; ask this as a normal user turn if workspace inspection or commands are needed"

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
		go runStartupMaintenance(cmd, cfg)
	}

	permBridge := tui.NewPermissionBridge()
	limitBridge := tui.NewLimitBridge()
	backgroundEvents := make(chan tui.Event, 64)
	runner, err := newTUIAgentRunner(cmd, cfg, permBridge, providerFlag, modelFlag, backgroundEvents)
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
	var initialMessages []tui.InitialMessage
	var loadMessages func(context.Context) ([]tui.InitialMessage, error)
	if !selectSessionOnStart && strings.TrimSpace(resume) != "" {
		loadMessages = func(context.Context) ([]tui.InitialMessage, error) {
			return runner.Messages(), nil
		}
	} else {
		initialMessages = runner.Messages()
	}
	err = tui.Run(cmd.Context(), tui.Options{
		Input:            cmd.InOrStdin(),
		Output:           cmd.OutOrStdout(),
		Runner:           runner,
		Permissions:      permBridge.Requests(),
		Limits:           limitBridge.Requests(),
		BackgroundEvents: backgroundEvents,
		Provider:         runner.Provider(),
		Providers:        runner.Providers(),
		Model:            runner.model,
		Models:           runner.Models(),
		Effort:           runner.Effort(),
		Efforts:          runner.Efforts(),
		ApprovalModel:    runner.ApprovalModel(),
		ApprovalModels:   runner.ApprovalModels(),
		SmallModel:       runner.SmallModel(),
		SmallModels:      runner.SmallModels(),
		Messages:         initialMessages,
		LoadMessages:     loadMessages,
		Turn:             runner.Turn(),
		ExecutionMode:    string(runner.mode),
		Cwd:              cwd,
		Theme:            cfg.TUI.Theme,
		EventTimeout:     runner.eventTimeout,
		SelectSession:    selectSessionOnStart,
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
	autoMemoryProvider   provider.Provider
	autoMemoryModel      string
	smallModels          []string
	smallUsesCurrent     bool
	contextCfg           config.ContextConfig
	memoryAutoScheduler  *agent.MemoryAutoScheduler
	backgroundEvents     chan<- tui.Event
	tools                *toolRuntime
	mode                 execution.Mode
	modeMu               sync.RWMutex
	eventTimeout         time.Duration
	permission           *permission.Manager
	state                *chatSessionState
	closedStore          bool
	maxTurns             int
	limitAsker           agent.LimitAsker
	providerCheckMu      sync.Mutex
	providerChecks       map[string]providerCheck

	// cachedMessages holds the reconstructed InitialMessages for the loaded
	// session. Populated lazily by Messages() so we only scan the rollout once
	// per session-load instead of on every sessionState() call.
	cachedMessages        []tui.InitialMessage
	cachedMessagesSession string
}

func newTUIAgentRunner(cmd *cobra.Command, cfg *config.Config, asker permission.Asker, providerFlag, modelFlag string, backgroundEvents chan<- tui.Event) (*tuiAgentRunner, error) {
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
	providerCheckResult := checkProvider(cmd.Context(), providerName, providerCfg)
	models := mergeModelCandidates(
		model,
		configuredProviderModels(providerCfg, ""),
		providerCheckResult.Models,
	)
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
	autoMemorySetup, err := newAutoMemorySetup(cmd.Context(), cfg, providerName, providerCfg, model)
	if err != nil {
		return nil, err
	}
	smallModels := mergeModelCandidates(autoMemorySetup.Model, []string{model}, models)
	tools, err := newToolRuntime(cmd.Context(), cfg)
	if err != nil {
		return nil, err
	}
	writeToolWarnings(cmd.ErrOrStderr(), tools.Warnings)
	projectRulesPath, err := permission.ProjectRulesPath(tools.Workspace)
	if err != nil {
		return nil, err
	}
	perm, err := permission.NewManager(permission.Options{
		Asker:            asker,
		ApprovalAgent:    approvalSetup.Agent,
		ProjectRulesPath: projectRulesPath,
	})
	if err != nil {
		return nil, err
	}
	providerChecks := map[string]providerCheck{}
	providerChecks[providerCheckKey(providerName, providerCfg)] = providerCheckResult
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
		autoMemoryProvider:   autoMemorySetup.Provider,
		autoMemoryModel:      autoMemorySetup.Model,
		smallModels:          smallModels,
		smallUsesCurrent:     autoMemorySetup.UsesCurrentModel,
		contextCfg:           cfg.Context,
		memoryAutoScheduler:  agent.NewMemoryAutoScheduler(),
		backgroundEvents:     backgroundEvents,
		tools:                tools,
		mode:                 mode,
		eventTimeout:         effectiveTUIEventTimeout(providerCfg.Timeout),
		permission:           perm,
		maxTurns:             cfg.MaxTurns,
		providerChecks:       providerChecks,
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

func (r *tuiAgentRunner) AnswerSideQuestion(ctx context.Context, req tui.SideQuestionRequest, events chan<- tui.Event) error {
	question := strings.TrimSpace(req.Question)
	if question == "" {
		return fmt.Errorf("btw question cannot be empty")
	}
	if r == nil || r.provider == nil {
		return fmt.Errorf("btw provider is unavailable")
	}
	workspace := ""
	if r.tools != nil {
		workspace = r.tools.Workspace
	}
	memoryMaxChars := 0
	if r.cfg != nil {
		memoryMaxChars = r.cfg.Memory.MaxChars
	}
	messages := agent.NoToolRuntimeContextMessages(agentRuntimeContext(workspace), workspace, memoryMaxChars)
	messages = append(messages, message.Text(message.RoleSystem, sideQuestionSystemPrompt))
	messages = append(messages, r.sideQuestionHistory()...)
	messages = append(messages, sideQuestionHistoryMessages(req.History)...)
	messages = append(
		messages,
		message.Text(message.RoleUser, question),
	)
	stream, err := r.provider.Chat(ctx, provider.Request{
		Model:     r.model,
		Messages:  messages,
		Tools:     nil,
		Reasoning: cloneReasoningConfig(r.reasoning),
	})
	if err != nil {
		return fmt.Errorf("btw provider %q chat: %w", r.providerName, err)
	}
	consumeErr := consumeSideQuestionStream(ctx, stream, events)
	closeErr := stream.Close()
	if consumeErr != nil {
		return consumeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

func (r *tuiAgentRunner) sideQuestionHistory() []message.Message {
	if r == nil || r.state == nil {
		return nil
	}
	return sideQuestionTextHistory(r.state.history)
}

func sideQuestionTextHistory(history []message.Message) []message.Message {
	var out []message.Message
	for _, msg := range history {
		switch msg.Role {
		case message.RoleUser, message.RoleAssistant:
			text := strings.TrimSpace(msg.Text())
			if text == "" {
				continue
			}
			out = append(out, message.Text(msg.Role, text))
		}
	}
	return out
}

func sideQuestionHistoryMessages(history []tui.SideQuestionMessage) []message.Message {
	var out []message.Message
	for _, item := range history {
		question := strings.TrimSpace(item.Question)
		answer := strings.TrimSpace(item.Answer)
		if question == "" || answer == "" {
			continue
		}
		out = append(
			out,
			message.Text(message.RoleUser, question),
			message.Text(message.RoleAssistant, item.Answer),
		)
	}
	return out
}

func consumeSideQuestionStream(ctx context.Context, stream provider.Stream, events chan<- tui.Event) error {
	var text strings.Builder
	for {
		event, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		switch event.Type {
		case provider.EventTextDelta:
			nextText := text.String() + event.Text
			if sideQuestionLooksLikeToolMarkup(nextText) {
				return errors.New(sideQuestionToolAttemptMessage)
			}
			text.WriteString(event.Text)
			sendTUIEvent(ctx, events, tui.Event{Type: tui.EventDeltaText, Text: event.Text})
		case provider.EventReasoningDelta, provider.EventUsage:
			continue
		case provider.EventDone:
			if strings.TrimSpace(text.String()) == "" {
				return fmt.Errorf("btw response was empty")
			}
			sendTUIEvent(ctx, events, tui.Event{Type: tui.EventDone, Text: text.String()})
			return nil
		case provider.EventToolCall:
			name := strings.TrimSpace(event.ToolName)
			if name == "" {
				name = "tool"
			}
			return fmt.Errorf("%s: %s", sideQuestionToolAttemptMessage, name)
		case provider.EventError:
			if event.Err != nil {
				return event.Err
			}
			return fmt.Errorf("btw provider returned an error event")
		default:
			return fmt.Errorf("btw provider returned unsupported event type %q", event.Type)
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		return fmt.Errorf("btw response was empty")
	}
	sendTUIEvent(ctx, events, tui.Event{Type: tui.EventDone, Text: text.String()})
	return nil
}

func sideQuestionLooksLikeToolMarkup(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{
		"<tool_use",
		"</tool_use",
		"<function=",
		"</function>",
		"<invoke",
		"</invoke>",
		"<tool_name>",
		"</tool_name>",
		"<tool>",
		"</tool>",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	for _, name := range []string{"ls", "read", "glob", "grep", "bash", "edit", "write", "task"} {
		if strings.Contains(lower, "<name>"+name+"</name>") {
			return true
		}
		if strings.Contains(lower, "<"+name) && strings.Contains(lower, "</"+name+">") {
			return true
		}
	}
	return false
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
	resolvedReasoning := cloneReasoningConfig(r.reasoning)
	maxContext := modelinfo.Resolve(r.providerName, r.providerCfg, r.model).MaxContextTokens
	runtime := agentRuntimeContext(r.tools.Workspace)
	hooksRunner := hook.New(r.cfg.Hooks)
	fileHistory, err := newFileHistoryManager(ctx, r.tools.Workspace, r.state.sessionID, r.state.rollout)
	if err != nil {
		return nil, err
	}
	subRunner := &cliSubagentRunner{
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
	}
	a, err := agent.New(agent.Options{
		Provider:            r.provider,
		Tools:               r.tools.Registry,
		Permission:          r.permission,
		Rollout:             r.state.rollout,
		Model:               r.model,
		Mode:                r.currentMode(),
		ModeFunc:            r.currentMode,
		MaxTurns:            r.maxTurns,
		LimitAsker:          r.limitAsker,
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
		SubagentRunner:      subRunner,
		FileHistory:         fileHistory,
		Events: func(event agent.Event) {
			sendTUIEvent(ctx, events, convertAgentEvent(event))
		},
		BackgroundEvents: r.backgroundEventSink(ctx),
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
			Provider:  sess.Provider,
			Model:     sess.Model,
			UpdatedAt: sess.UpdatedAt,
			Current:   sess.ID == current,
		})
	}
	return out, nil
}

func (r *tuiAgentRunner) SearchSessions(ctx context.Context, query string, limit int) (string, error) {
	st, closeStore, err := r.sessionSearchStore()
	if err != nil {
		return "", err
	}
	if closeStore != nil {
		defer closeStore()
	}
	sessions, err := st.ListAllSessions(ctx)
	if err != nil {
		return "", fmt.Errorf("list sessions: %w", err)
	}
	ro, err := rollout.New(st)
	if err != nil {
		return "", fmt.Errorf("open rollout: %w", err)
	}
	matches, err := searchSessions(ctx, ro, sessions, query, limit)
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", nil
	}
	var out strings.Builder
	for _, m := range matches {
		fmt.Fprintf(&out, "%s  turn %d  %s  %s\n", m.Session.ID, m.Turn, m.Type, m.Time.Format(time.RFC3339))
		snippet := m.Snippet
		if len(snippet) > 120 {
			snippet = snippet[:120] + "…"
		}
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		fmt.Fprintf(&out, "  %s\n", snippet)
	}
	return out.String(), nil
}

func (r *tuiAgentRunner) sessionSearchStore() (*store.Store, func() error, error) {
	if r != nil && r.state != nil && r.state.store != nil && !r.closedStore {
		return r.state.store, nil, nil
	}
	path, err := store.DefaultPath()
	if err != nil {
		return nil, nil, fmt.Errorf("locate session store: %w", err)
	}
	st, err := store.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return st, st.Close, nil
}

func (r *tuiAgentRunner) Doctor(ctx context.Context) (string, error) {
	var liveStatus []mcptool.ServerStatus
	if r.tools != nil && r.tools.MCPConnections != nil {
		liveStatus = r.tools.MCPConnections.Status()
	}
	return renderDoctorTextWithLive(ctx, r.cfg, true, false, liveStatus)
}

func (r *tuiAgentRunner) NewSession(ctx context.Context) (tui.SessionState, error) {
	state, err := startChatRollout(r.cmd, "", r.providerName, r.model, chatOptions{})
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
	state, err := startChatRollout(r.cmd, "", r.providerName, r.model, chatOptions{SessionID: id})
	if err != nil {
		return tui.SessionState{}, err
	}
	cwd, err := currentWorkspace()
	if err != nil {
		_ = state.store.Close()
		return tui.SessionState{}, err
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
	if err := r.restoreSessionProviderModel(ctx, state.session); err != nil {
		return tui.SessionState{}, err
	}
	return r.sessionState(), nil
}

func (r *tuiAgentRunner) restoreSessionProviderModel(ctx context.Context, sess store.Session) error {
	sessionProvider := strings.TrimSpace(sess.Provider)
	sessionModel := strings.TrimSpace(sess.Model)
	if sessionProvider == "" {
		sessionProvider = r.inferSessionProvider(ctx, sessionModel)
	}
	if sessionProvider == "" {
		if sessionModel != "" {
			r.model = sessionModel
			r.models = appendModelCandidate(r.models, sessionModel)
			r.refreshReasoning()
		}
		return nil
	}
	if _, err := r.setProviderModel(ctx, sessionProvider, sessionModel); err != nil {
		return err
	}
	return nil
}

func (r *tuiAgentRunner) inferSessionProvider(ctx context.Context, model string) string {
	model = strings.TrimSpace(model)
	if r == nil || r.cfg == nil || model == "" {
		return ""
	}
	for _, providerName := range sortedProviderNames(r.cfg.Providers) {
		providerCfg := r.cfg.Providers[providerName]
		if modelInList(configuredProviderModels(providerCfg, ""), model) {
			return providerName
		}
	}
	for _, providerName := range sortedProviderNames(r.cfg.Providers) {
		providerCfg := r.cfg.Providers[providerName]
		check := r.checkProvider(ctx, providerName, providerCfg)
		if modelInList(check.Models, model) {
			return providerName
		}
	}
	return ""
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
		ID:        r.state.sessionID,
		Provider:  r.providerName,
		Providers: r.Providers(),
		Model:     r.model,
		Models:    r.Models(),
		Effort:    r.Effort(),
		Efforts:   r.Efforts(),
		Turn:      r.Turn(),
		Messages:  r.Messages(),
	}
}

func (r *tuiAgentRunner) SetModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("model cannot be empty")
	}
	candidates := r.models
	if r != nil && r.cmd != nil {
		candidates = mergeModelCandidates(model, candidates, r.providerModels(r.cmd.Context(), r.providerName, r.providerCfg, ""))
	}
	if !modelInList(candidates, model) {
		return fmt.Errorf("model %q is not available for the current provider", model)
	}
	r.models = candidates
	r.model = model
	r.models = appendModelCandidate(r.models, model)
	r.summaryModel = model
	if r.smallUsesCurrent {
		r.autoMemoryModel = model
	}
	r.smallModels = mergeModelCandidates(r.autoMemoryModel, []string{r.model}, r.smallModels, r.models)
	r.refreshReasoning()
	return r.persistSessionProviderModel(r.cmd.Context())
}

func (r *tuiAgentRunner) persistSessionProviderModel(ctx context.Context) error {
	if r == nil || r.state == nil || r.closedStore {
		return nil
	}
	r.state.session.Provider = r.providerName
	r.state.session.Model = r.model
	r.state.session.UpdatedAt = time.Now().UTC()
	return r.state.store.UpdateSession(ctx, r.state.session)
}

func (r *tuiAgentRunner) providerSelection() tui.ProviderSelection {
	return tui.ProviderSelection{
		Provider:  r.providerName,
		Providers: r.Providers(),
		Model:     r.model,
		Models:    r.Models(),
		Effort:    r.Effort(),
		Efforts:   r.Efforts(),
	}
}

func (r *tuiAgentRunner) RefreshModels(ctx context.Context) ([]string, error) {
	if r == nil {
		return nil, fmt.Errorf("model refresh is unavailable")
	}
	return r.providerModels(ctx, r.providerName, r.providerCfg, r.model), nil
}

func (r *tuiAgentRunner) SetProvider(providerName, model string) (tui.ProviderSelection, error) {
	if r == nil || r.cmd == nil {
		return tui.ProviderSelection{}, fmt.Errorf("provider switching is unavailable")
	}
	return r.setProviderModel(r.cmd.Context(), providerName, model)
}

func (r *tuiAgentRunner) setProviderModel(ctx context.Context, providerName, model string) (tui.ProviderSelection, error) {
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
	models := r.providerModels(r.cmd.Context(), providerName, providerCfg, selectedModel)
	info := modelinfo.Resolve(providerName, providerCfg, selectedModel)
	summarySetup, err := newSummarySetup(r.cmd.Context(), r.cfg, providerName, providerCfg, selectedModel)
	if err != nil {
		return tui.ProviderSelection{}, err
	}
	autoMemorySetup, err := newAutoMemorySetup(r.cmd.Context(), r.cfg, providerName, providerCfg, selectedModel)
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
	r.autoMemoryProvider = autoMemorySetup.Provider
	r.autoMemoryModel = autoMemorySetup.Model
	r.smallModels = mergeModelCandidates(autoMemorySetup.Model, []string{selectedModel}, models)
	r.smallUsesCurrent = autoMemorySetup.UsesCurrentModel
	r.approvalProviderName = approvalSetup.ProviderName
	r.approvalProviderCfg = approvalSetup.ProviderConfig
	r.approvalModel = approvalSetup.Model
	r.approvalModels = approvalSetup.Models
	if r.approvalProviderName != "" {
		r.approvalModels = r.providerModels(r.cmd.Context(), r.approvalProviderName, r.approvalProviderCfg, r.approvalModel)
	}
	r.approvalReasoning = r.cfg.ApprovalAgent.Reasoning
	if r.permission != nil {
		r.permission.SetApprovalAgent(approvalSetup.Agent)
	}
	r.refreshReasoning()
	if err := r.persistSessionProviderModel(ctx); err != nil {
		return tui.ProviderSelection{}, err
	}
	return r.providerSelection(), nil
}

func (r *tuiAgentRunner) modelForProviderSwitch(providerName string, providerCfg config.ProviderConfig, model string) (string, error) {
	model = strings.TrimSpace(model)
	if model != "" {
		return model, nil
	}
	if r != nil && r.cfg != nil && strings.TrimSpace(r.cfg.DefaultProvider) == providerName && strings.TrimSpace(r.cfg.DefaultModel) != "" {
		return strings.TrimSpace(r.cfg.DefaultModel), nil
	}
	candidates := r.providerModels(r.cmd.Context(), providerName, providerCfg, "")
	currentModel := strings.TrimSpace(r.model)
	if currentModel != "" && (len(candidates) == 0 || modelInList(candidates, currentModel)) {
		return currentModel, nil
	}
	if configured := firstConfiguredProviderModel(providerCfg); configured != "" {
		return configured, nil
	}
	if len(candidates) > 0 {
		return candidates[0], nil
	}
	return r.selectProviderModel(r.cmd.Context(), providerName, providerCfg, "")
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
	ctx := context.Background()
	if r.cmd != nil {
		ctx = r.cmd.Context()
	}
	r.approvalModels = mergeModelCandidates(r.approvalModel, r.approvalModels, r.providerModels(ctx, r.approvalProviderName, r.approvalProviderCfg, ""))
	if !modelInList(r.approvalModels, model) {
		r.approvalModels = r.providerModels(ctx, r.approvalProviderName, r.approvalProviderCfg, "")
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

func (r *tuiAgentRunner) RefreshApprovalModels(ctx context.Context) ([]string, error) {
	if r == nil || r.approvalProviderName == "" {
		return nil, fmt.Errorf("approval provider is not configured")
	}
	r.approvalModels = r.providerModels(ctx, r.approvalProviderName, r.approvalProviderCfg, r.approvalModel)
	return append([]string(nil), r.approvalModels...), nil
}

func (r *tuiAgentRunner) SetSmallModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("small model cannot be empty")
	}
	if r == nil || r.providerName == "" {
		return fmt.Errorf("small model switching is unavailable")
	}
	ctx := context.Background()
	if r.cmd != nil {
		ctx = r.cmd.Context()
	}
	r.smallModels = mergeModelCandidates(r.autoMemoryModel, []string{r.model}, r.smallModels, r.providerModels(ctx, r.providerName, r.providerCfg, ""))
	if !modelInList(r.smallModels, model) {
		return fmt.Errorf("small model %q is not available for the current provider", model)
	}
	r.autoMemoryModel = model
	r.smallUsesCurrent = false
	r.smallModels = appendModelCandidate(r.smallModels, model)
	return nil
}

func (r *tuiAgentRunner) RefreshSmallModels(ctx context.Context) ([]string, error) {
	if r == nil || r.providerName == "" {
		return nil, fmt.Errorf("small model switching is unavailable")
	}
	r.smallModels = mergeModelCandidates(r.autoMemoryModel, []string{r.model}, r.providerModels(ctx, r.providerName, r.providerCfg, ""))
	return append([]string(nil), r.smallModels...), nil
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

func (r *tuiAgentRunner) SmallModel() string {
	if r == nil {
		return ""
	}
	return r.autoMemoryModel
}

func (r *tuiAgentRunner) SmallModels() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.smallModels...)
}

func (r *tuiAgentRunner) newApprovalAgent(model string) (approval.Agent, error) {
	p, err := provider.New(r.approvalProviderName, r.approvalProviderCfg)
	if err != nil {
		return nil, fmt.Errorf("create approval provider %q: %w", r.approvalProviderName, err)
	}
	reasoningCfg := modelinfo.RequestConfig(r.approvalReasoning, modelinfo.Resolve(r.approvalProviderName, r.approvalProviderCfg, model))
	return approval.NewProviderAgentWithReasoning(p, model, reasoningCfg)
}

func configuredProviderModels(providerCfg config.ProviderConfig, current string) []string {
	models := make([]string, 0, len(providerCfg.Models))
	for model := range providerCfg.Models {
		models = append(models, model)
	}
	sort.Strings(models)
	return appendModelCandidate(models, current)
}

func (r *tuiAgentRunner) providerModels(ctx context.Context, providerName string, providerCfg config.ProviderConfig, current string) []string {
	result := r.checkProvider(ctx, providerName, providerCfg)
	return mergeModelCandidates(current, configuredProviderModels(providerCfg, ""), result.Models)
}

func (r *tuiAgentRunner) selectProviderModel(ctx context.Context, providerName string, providerCfg config.ProviderConfig, model string) (string, error) {
	model = strings.TrimSpace(model)
	if model != "" {
		return model, nil
	}
	if configured := firstConfiguredProviderModel(providerCfg); configured != "" {
		return configured, nil
	}
	check := r.checkProvider(ctx, providerName, providerCfg)
	if len(check.Models) > 0 {
		return check.Models[0], nil
	}
	if !providerRequiresModel(providerCfg.Type) {
		return "", nil
	}
	return "", missingModelError(providerName, check)
}

func (r *tuiAgentRunner) checkProvider(ctx context.Context, providerName string, providerCfg config.ProviderConfig) providerCheck {
	if r == nil {
		return checkProvider(ctx, providerName, providerCfg)
	}
	key := providerCheckKey(providerName, providerCfg)
	r.providerCheckMu.Lock()
	if r.providerChecks == nil {
		r.providerChecks = map[string]providerCheck{}
	}
	if cached, ok := r.providerChecks[key]; ok {
		r.providerCheckMu.Unlock()
		return cached
	}
	r.providerCheckMu.Unlock()

	result := checkProvider(ctx, providerName, providerCfg)

	r.providerCheckMu.Lock()
	r.providerChecks[key] = result
	r.providerCheckMu.Unlock()
	return result
}

func providerCheckKey(providerName string, providerCfg config.ProviderConfig) string {
	return strings.Join([]string{
		strings.TrimSpace(providerName),
		strings.TrimSpace(providerCfg.Type),
		strings.TrimSpace(providerCfg.BaseURL),
	}, "\x00")
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

func mergeModelCandidates(current string, groups ...[]string) []string {
	current = strings.TrimSpace(current)
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
	add(current)
	for _, group := range groups {
		for _, candidate := range group {
			add(candidate)
		}
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
	cwd, err := currentWorkspace()
	if err != nil {
		return nil, err
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

		if event.Type == rollout.TypeToolResult {
			var payload rollout.ToolResultPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return fmt.Errorf("decode rollout tool_result event %s: %w", event.ID, err)
			}
			out = appendToolResultForTUI(out, toolUses, event.Turn, payload.ToolUseID, payload.ToolName, tool.Result{
				Content:        payload.Output,
				IsError:        payload.IsError,
				Files:          payload.Files,
				Truncated:      payload.Truncated,
				OriginalBytes:  payload.OriginalBytes,
				FullOutputPath: payload.FullOutputPath,
			})
			return nil
		}

		if event.Type == rollout.TypeSummary {
			return nil
		}
		msg, ok, err := rollout.MessageFromEvent(event)
		if err != nil {
			return err
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
			out = appendToolResultForTUI(out, toolUses, turn, block.ToolUseID, "", tool.Result{
				Content: block.Output,
				IsError: block.IsError,
			})
		}
	}
	return out
}

func appendToolResultForTUI(out []tui.InitialMessage, toolUses map[string]message.ContentBlock, turn int, toolUseID, toolName string, result tool.Result) []tui.InitialMessage {
	toolUse := toolUses[toolUseID]
	toolName = fallbackString(toolName, toolUse.ToolName)
	if strings.TrimSpace(toolName) == "" {
		toolName = "tool"
	}
	status := "done"
	if result.IsError {
		status = "failed"
	}
	summary, detail := agent.ToolActivityResultWithInput(toolName, toolUse.Input, result)
	return append(out, tui.InitialMessage{
		Turn:         turn,
		ActivityKind: "tool",
		ToolUseID:    toolUseID,
		ToolName:     toolName,
		Status:       status,
		Summary:      summary,
		Content:      detail,
		IsError:      result.IsError,
	})
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
	case agent.EventToolPartialOutput:
		return tui.Event{
			Type:      tui.EventToolPartialOutput,
			ToolUseID: event.ToolUseID,
			ToolName:  event.ToolName,
			Status:    event.Status,
			Summary:   event.Summary,
			Content:   event.Content,
			IsError:   event.IsError,
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
	defer func() {
		// Background post-turn work can outlive the TUI event consumer. Sending
		// to a closed channel must never take the whole terminal down.
		_ = recover()
	}()
	select {
	case events <- event:
	case <-ctx.Done():
	}
}
