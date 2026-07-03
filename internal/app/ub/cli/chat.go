package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/feimingxliu/ub/internal/app/ub/agent"
	"github.com/feimingxliu/ub/internal/pkg/core/config"
	"github.com/feimingxliu/ub/internal/pkg/core/execution"
	"github.com/feimingxliu/ub/internal/pkg/core/message"
	"github.com/feimingxliu/ub/internal/pkg/llm/provider"
	"github.com/feimingxliu/ub/internal/pkg/runtime/hook"
	"github.com/feimingxliu/ub/internal/pkg/runtime/permission"
	"github.com/feimingxliu/ub/internal/pkg/workspace/rollout"
	"github.com/feimingxliu/ub/internal/pkg/workspace/store"
	"github.com/spf13/cobra"
)

// chatOptions configures a headless chat session.
type chatOptions struct {
	SessionID string
	New       bool
}

// chatSessionState holds the store, rollout, and conversation state for a
// single headless chat session. It is created by runChat and closed when the
// command finishes.
type chatSessionState struct {
	store          *store.Store
	rollout        *rollout.SQLite
	session        store.Session
	history        []message.Message
	contextHistory []message.Message
	nextTurn       int
	sessionID      string
}

// Close releases the rollout writer and store, returning the first error
// encountered. It is idempotent: calling Close on an already-closed state
// is a no-op.
func (s *chatSessionState) Close() error {
	if s == nil {
		return nil
	}
	var err error
	if s.rollout != nil {
		if closeErr := s.rollout.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		s.rollout = nil
	}
	if s.store != nil {
		if closeErr := s.store.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		s.store = nil
	}
	return err
}

// runAgent executes a single headless agent turn: it resolves the provider
// and model from config/flags, creates a session, builds the agent with the
// full tool set, runs one prompt, and prints the result to stdout.
func runAgent(cmd *cobra.Command, prompt, providerFlag, modelFlag string) error {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fmt.Errorf("prompt required: pass -p/--prompt")
	}
	cfg, _, err := loadConfigForCommand(cmd)
	if err != nil {
		return err
	}
	runStartupMaintenance(cmd, cfg)
	mainRole, err := resolveMainModelRole(cmd.Context(), cfg, providerFlag, modelFlag)
	if err != nil {
		return err
	}
	providerName := mainRole.ProviderName
	providerCfg := mainRole.ProviderConfig
	model := mainRole.Model
	providers := newProviderCache()
	p, err := providers.Get(providerName, providerCfg)
	if err != nil {
		return fmt.Errorf("create provider %q: %w", providerName, err)
	}
	mode, err := execution.ParseMode(cfg.ExecutionMode)
	if err != nil {
		return err
	}
	tools, err := newToolRuntime(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer tools.Close()
	writeToolWarnings(cmd.ErrOrStderr(), tools.Warnings)
	approvalAgent, err := newApprovalAgentFromConfig(cmd.Context(), cfg, providerName, model, providers)
	if err != nil {
		return err
	}
	summarySetup, err := newSummarySetup(cmd.Context(), cfg, providerName, providerCfg, model, providers)
	if err != nil {
		return err
	}
	autoMemorySetup, err := newAutoMemorySetup(cmd.Context(), cfg, providerName, providerCfg, model, providers)
	if err != nil {
		return err
	}
	projectRulesPath, err := permission.ProjectRulesPath(tools.Workspace)
	if err != nil {
		return err
	}
	perm, err := permission.NewManager(permission.Options{
		Asker:            autoAllowAsker{},
		ApprovalAgent:    approvalAgent,
		ProjectRulesPath: projectRulesPath,
	})
	if err != nil {
		return err
	}
	state, err := startChatRollout(cmd, prompt, providerName, model, chatOptions{})
	if err != nil {
		return err
	}
	defer state.Close()
	fileHistory, err := newFileHistoryManager(cmd.Context(), tools.Workspace, state.sessionID, state.rollout)
	if err != nil {
		return err
	}

	hooksRunner := hook.New(cfg.Hooks)
	memoryAutoScheduler := agent.NewMemoryAutoScheduler()
	factory := agent.NewFactory(agent.Options{
		Provider:            p,
		Tools:               tools.Registry,
		Permission:          perm,
		Rollout:             state.rollout,
		Model:               model,
		Mode:                mode,
		MaxTurns:            cfg.MaxTurns,
		Reasoning:           mainRole.cloneReasoning(),
		MaxContextTokens:    mainRole.MaxContextTokens,
		SummaryProvider:     summarySetup.Provider,
		SummaryModel:        summarySetup.Model,
		AutoMemoryProvider:  autoMemorySetup.Provider,
		AutoMemoryModel:     autoMemorySetup.Model,
		Context:             cfg.Context,
		Prompt:              cfg.Prompt,
		Runtime:             agentRuntimeContext(tools.Workspace),
		Hooks:               hooksRunner,
		WorkspaceRoot:       tools.Workspace,
		MemoryMaxChars:      cfg.Memory.MaxChars,
		Memory:              cfg.Memory,
		MemoryAutoScheduler: memoryAutoScheduler,
		FileHistory:         fileHistory,
	})
	subRunner := &cliSubagentRunner{
		factory:          factory,
		provider:         p,
		tools:            tools.Registry,
		permission:       perm,
		model:            model,
		mode:             mode,
		reasoningCfg:     mainRole.cloneReasoning(),
		maxContextTokens: mainRole.MaxContextTokens,
		contextCfg:       cfg.Context,
		promptCfg:        cfg.Prompt,
		runtime:          agentRuntimeContext(tools.Workspace),
		hooks:            hooksRunner,
		defaultMaxTurns:  cfg.MaxTurns,
		workspaceRoot:    tools.Workspace,
		memoryMaxChars:   cfg.Memory.MaxChars,
		fileHistory:      fileHistory,
		rollout:          state.rollout,
	}
	a, err := factory.New(func(opts *agent.Options) {
		opts.SubagentRunner = subRunner
	})
	if err != nil {
		return err
	}
	result, err := a.Run(cmd.Context(), agent.Request{
		SessionID:      state.sessionID,
		Turn:           state.nextTurn,
		History:        state.history,
		ContextHistory: state.contextHistory,
		Prompt:         prompt,
	})
	if err != nil {
		_ = finishChatSession(cmd, state, prompt, providerName, model)
		return err
	}
	if _, err := io.WriteString(cmd.OutOrStdout(), result.Text); err != nil {
		return err
	}
	_ = a.DrainAutoMemory(cmd.Context())
	return finishChatSession(cmd, state, prompt, providerName, model)
}

func runChat(cmd *cobra.Command, promptArg, providerFlag, modelFlag string, opts chatOptions) error {
	if opts.New && strings.TrimSpace(opts.SessionID) != "" {
		return fmt.Errorf("cannot use --new with --session")
	}
	prompt, err := readChatPrompt(cmd, promptArg)
	if err != nil {
		return err
	}

	cfg, _, err := loadConfigForCommand(cmd)
	if err != nil {
		return err
	}
	if strings.TrimSpace(opts.SessionID) == "" {
		runStartupMaintenance(cmd, cfg)
	}
	mainRole, err := resolveMainModelRole(cmd.Context(), cfg, providerFlag, modelFlag)
	if err != nil {
		return err
	}
	providerName := mainRole.ProviderName
	providerCfg := mainRole.ProviderConfig
	model := mainRole.Model
	providers := newProviderCache()
	p, err := providers.Get(providerName, providerCfg)
	if err != nil {
		return fmt.Errorf("create provider %q: %w", providerName, err)
	}

	state, err := startChatRollout(cmd, prompt, providerName, model, opts)
	if err != nil {
		return err
	}
	defer state.Close()

	userMsg := message.Text(message.RoleUser, prompt)
	event, err := rollout.UserMessage(state.sessionID, state.nextTurn, userMsg)
	if err != nil {
		return err
	}
	if err := state.rollout.Append(cmd.Context(), event); err != nil {
		return err
	}

	requestMessages := append(cloneMessages(state.contextHistory), userMsg)
	stream, err := p.Chat(cmd.Context(), provider.Request{
		Model:     model,
		Messages:  requestMessages,
		Reasoning: mainRole.cloneReasoning(),
	})
	if err != nil {
		return recordChatError(cmd, state, fmt.Errorf("provider %q chat: %w", providerName, err))
	}
	defer stream.Close()

	var assistant strings.Builder
	for {
		event, err := stream.Next(cmd.Context())
		if err == io.EOF {
			if err := recordAssistantMessage(cmd, state.rollout, state.sessionID, state.nextTurn, assistant.String()); err != nil {
				return err
			}
			if err := finishChatSession(cmd, state, prompt, providerName, model); err != nil {
				return err
			}
			return nil
		}
		if err != nil {
			if recordErr := recordAssistantMessage(cmd, state.rollout, state.sessionID, state.nextTurn, assistant.String()); recordErr != nil {
				return recordErr
			}
			return recordChatError(cmd, state, fmt.Errorf("provider %q stream: %w", providerName, err))
		}
		switch event.Type {
		case provider.EventTextDelta:
			if _, err := io.WriteString(cmd.OutOrStdout(), event.Text); err != nil {
				return recordChatError(cmd, state, err)
			}
			assistant.WriteString(event.Text)
		case provider.EventReasoningDelta:
			continue
		case provider.EventUsage:
			if event.Usage != nil {
				usageEvent, err := rollout.UsageWithDetails(state.sessionID, state.nextTurn, rollout.UsagePayload{
					InputTokens:      event.Usage.InputTokens,
					OutputTokens:     event.Usage.OutputTokens,
					ReasoningTokens:  event.Usage.ReasoningTokens,
					CacheReadTokens:  event.Usage.CacheReadTokens,
					CacheWriteTokens: event.Usage.CacheWriteTokens,
				})
				if err != nil {
					return err
				}
				if err := state.rollout.Append(cmd.Context(), usageEvent); err != nil {
					return err
				}
			}
			continue
		case provider.EventDone:
			if err := recordAssistantMessage(cmd, state.rollout, state.sessionID, state.nextTurn, assistant.String()); err != nil {
				return err
			}
			if err := finishChatSession(cmd, state, prompt, providerName, model); err != nil {
				return err
			}
			return nil
		case provider.EventToolCall:
			var toolErr error
			if event.ToolName == "" {
				toolErr = fmt.Errorf("ub chat does not execute tool calls yet")
			} else {
				toolErr = fmt.Errorf("ub chat does not execute tool calls yet: received %q", event.ToolName)
			}
			return recordChatError(cmd, state, toolErr)
		case provider.EventError:
			var eventErr error
			if event.Err != nil {
				eventErr = event.Err
			} else {
				eventErr = fmt.Errorf("provider returned error event")
			}
			return recordChatError(cmd, state, eventErr)
		default:
			return recordChatError(cmd, state, fmt.Errorf("provider returned unsupported event type %q", event.Type))
		}
	}
}

func startChatRollout(cmd *cobra.Command, prompt, providerName, model string, opts chatOptions) (*chatSessionState, error) {
	path, err := store.DefaultPath()
	if err != nil {
		return nil, fmt.Errorf("locate session store: %w", err)
	}
	st, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	ro, err := rollout.New(st)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	closeOpened := func() {
		_ = ro.Close()
		_ = st.Close()
	}
	cwd, err := currentWorkspace()
	if err != nil {
		closeOpened()
		return nil, err
	}

	if sessionID := strings.TrimSpace(opts.SessionID); sessionID != "" {
		sess, err := st.GetSession(cmd.Context(), sessionID)
		if errors.Is(err, store.ErrNotFound) {
			closeOpened()
			return nil, fmt.Errorf("session %q not found", sessionID)
		}
		if err != nil {
			closeOpened()
			return nil, err
		}
		history, contextHistory, nextTurn, err := readChatHistory(cmd, ro, sessionID)
		if err != nil {
			closeOpened()
			return nil, err
		}
		return &chatSessionState{
			store:          st,
			rollout:        ro,
			session:        *sess,
			history:        history,
			contextHistory: contextHistory,
			nextTurn:       nextTurn,
			sessionID:      sessionID,
		}, nil
	}

	sessionID := rollout.NewID("sess")
	now := time.Now().UTC()
	sess := store.Session{
		ID:        sessionID,
		Workspace: cwd,
		Title:     chatTitle(prompt),
		Provider:  providerName,
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := st.CreateSession(cmd.Context(), sess); err != nil {
		closeOpened()
		return nil, err
	}
	return &chatSessionState{
		store:          st,
		rollout:        ro,
		session:        sess,
		contextHistory: nil,
		nextTurn:       1,
		sessionID:      sessionID,
	}, nil
}

func readChatHistory(cmd *cobra.Command, ro *rollout.SQLite, sessionID string) ([]message.Message, []message.Message, int, error) {
	var history []message.Message
	var contextHistory []message.Message
	maxTurn := 0
	ctx := context.Background()
	if cmd != nil && cmd.Context() != nil {
		ctx = cmd.Context()
	}
	if err := ro.ForEach(ctx, sessionID, func(event rollout.Event) error {
		if event.Turn > maxTurn {
			maxTurn = event.Turn
		}
		if event.Type == rollout.TypeSummary {
			msgs, ok, err := rollout.SummaryMessagesFromEvent(event)
			if err != nil {
				return err
			}
			if ok {
				contextHistory = msgs
			}
			return nil
		}
		msg, ok, err := rollout.MessageFromEvent(event)
		if err != nil {
			return err
		}
		if ok {
			history = append(history, msg)
			contextHistory = append(contextHistory, msg)
		}
		return nil
	}); err != nil {
		return nil, nil, 0, err
	}
	return history, contextHistory, maxTurn + 1, nil
}

func finishChatSession(cmd *cobra.Command, state *chatSessionState, prompt, providerName, model string) error {
	state.session.Provider = providerName
	state.session.Model = model
	state.session.UpdatedAt = time.Now().UTC()
	if state.session.Title == "" {
		state.session.Title = chatTitle(prompt)
	}
	return state.store.UpdateSession(cmd.Context(), state.session)
}

func recordAssistantMessage(cmd *cobra.Command, ro *rollout.SQLite, sessionID string, turn int, text string) error {
	if text == "" {
		return nil
	}
	event, err := rollout.AssistantMessage(sessionID, turn, message.Text(message.RoleAssistant, text))
	if err != nil {
		return err
	}
	return ro.Append(cmd.Context(), event)
}

func recordChatError(cmd *cobra.Command, state *chatSessionState, chatErr error) error {
	event, err := rollout.Error(state.sessionID, state.nextTurn, chatErr)
	if err != nil {
		return fmt.Errorf("record rollout error payload: %v; original error: %w", err, chatErr)
	}
	if err := state.rollout.Append(cmd.Context(), event); err != nil {
		return fmt.Errorf("record rollout error: %v; original error: %w", err, chatErr)
	}
	state.session.UpdatedAt = time.Now().UTC()
	if err := state.store.UpdateSession(cmd.Context(), state.session); err != nil {
		return fmt.Errorf("update session after chat error: %v; original error: %w", err, chatErr)
	}
	return chatErr
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

func chatTitle(prompt string) string {
	title := strings.TrimSpace(strings.Join(strings.Fields(prompt), " "))
	if title == "" {
		return "(empty prompt)"
	}
	const max = 60
	runes := []rune(title)
	if len(runes) <= max {
		return title
	}
	return string(runes[:max-3]) + "..."
}

func readChatPrompt(cmd *cobra.Command, promptArg string) (string, error) {
	if promptArg != "-" {
		return promptArg, nil
	}
	raw, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", fmt.Errorf("read stdin prompt: %w", err)
	}
	return string(raw), nil
}

func selectChatProvider(cfg *config.Config, providerFlag, modelFlag string) (string, string, error) {
	providerName := strings.TrimSpace(providerFlag)
	providerExplicit := providerName != ""
	model := strings.TrimSpace(modelFlag)
	if cfg != nil {
		if providerName == "" {
			providerName = strings.TrimSpace(cfg.DefaultProvider)
		}
		if providerName == "" {
			providerName = firstConfiguredProvider(cfg.Providers)
		}
		if model == "" && defaultModelAppliesToProvider(cfg, providerName, providerExplicit) {
			model = strings.TrimSpace(cfg.DefaultModel)
		}
	}
	if providerName == "" {
		return "", "", fmt.Errorf("provider required: set --provider, default_provider, or configure at least one provider")
	}
	return providerName, model, nil
}

func defaultModelAppliesToProvider(cfg *config.Config, providerName string, providerExplicit bool) bool {
	if cfg == nil || strings.TrimSpace(cfg.DefaultModel) == "" {
		return false
	}
	if !providerExplicit {
		return true
	}
	defaultProvider := strings.TrimSpace(cfg.DefaultProvider)
	return defaultProvider == "" || strings.TrimSpace(providerName) == defaultProvider
}

func firstConfiguredProvider(providers map[string]config.ProviderConfig) string {
	for _, name := range sortedProviderNames(providers) {
		if strings.TrimSpace(providers[name].Type) != "" {
			return name
		}
	}
	return ""
}
