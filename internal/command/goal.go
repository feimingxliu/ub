package command

import (
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/feimingxliu/ub/internal/agent"
	"github.com/feimingxliu/ub/internal/hook"
	execmode "github.com/feimingxliu/ub/internal/mode"
	"github.com/feimingxliu/ub/internal/permission"
	goaltool "github.com/feimingxliu/ub/internal/tool/goal"
	"github.com/spf13/cobra"
)

// runGoal executes a headless goal-mode session: it runs the agent with the
// user's prompt as the initial objective, then auto-continues turns until the
// goal is complete, blocked, or budget-limited.
func runGoal(cmd *cobra.Command, prompt, providerFlag, modelFlag string, tokenBudget, turnBudget int) error {
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
	mode, err := execmode.ParseMode(cfg.ExecutionMode)
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
	// Pre-create the goal so the agent starts in goal mode immediately.
	g := &goaltool.Goal{
		SessionID:   state.sessionID,
		Objective:   truncateGoalObjectiveCLI(prompt, 4000),
		Status:      goaltool.StatusActive,
		TokenBudget: tokenBudget,
		TurnBudget:  turnBudget,
	}
	if err := goaltool.Save(state.sessionID, g); err != nil {
		return fmt.Errorf("create goal: %w", err)
	}

	hooksRunner := hook.New(cfg.Hooks)
	memoryAutoScheduler := agent.NewMemoryAutoScheduler()
	contextWindow := newContextWindowResolver(providerName, providerCfg, model, mainRole.MaxContextTokens, p)
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
		ContextWindow:       contextWindow,
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

	// The initial prompt tells the agent to start working on the goal.
	initialPrompt := fmt.Sprintf("I have created a goal for you. Use get_goal to check its status, then work toward completing it. When done, call update_goal(status=\"complete\").\n\nObjective: %s", prompt)

	// Goal auto-continuation loop.
	currentPrompt := initialPrompt
	isAuto := false
	for {
		result, err := a.Run(cmd.Context(), agent.Request{
			SessionID:      state.sessionID,
			Turn:           state.nextTurn,
			History:        state.history,
			ContextHistory: state.contextHistory,
			Prompt:         currentPrompt,
			AutoTriggered:  isAuto,
		})
		if err != nil {
			_ = finishChatSession(cmd, state, prompt, providerName, model)
			return err
		}
		if _, err := io.WriteString(cmd.OutOrStdout(), result.Text); err != nil {
			return err
		}
		state.history = result.Messages
		state.contextHistory = result.ContextMessages
		state.nextTurn++
		// Record usage and check if goal should continue.
		if usageErr := goaltool.RecordUsage(state.sessionID, 0); usageErr != nil {
			slog.Warn("goal record usage error", "err", usageErr)
		}
		g, _ = goaltool.Load(state.sessionID)
		if g == nil || goaltool.IsTerminal(g.Status) {
			break
		}
		// Build continuation prompt for next turn.
		isAuto = true
		currentPrompt = agent.GoalContinuationPrompt(g)
		fmt.Fprintf(cmd.ErrOrStderr(), "\n[goal] continuing: %s (turns=%d, tokens=%d)\n", truncateGoalObjectiveCLI(g.Objective, 60), g.TurnsUsed, g.TokensUsed)
	}
	// Print final goal status.
	g, _ = goaltool.Load(state.sessionID)
	if g != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "\n[goal] status=%s turns=%d tokens=%d\n", g.Status, g.TurnsUsed, g.TokensUsed)
	}
	_ = a.DrainAutoMemory(cmd.Context())
	return finishChatSession(cmd, state, prompt, providerName, model)
}

func truncateGoalObjectiveCLI(objective string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(objective))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes]) + "..."
}

func newGoalCmd() *cobra.Command {
	var prompt string
	var providerName string
	var model string
	var tokenBudget int
	var turnBudget int

	cmd := &cobra.Command{
		Use:   "goal",
		Short: "Run a headless goal-mode session (autonomous multi-turn agent)",
		Long: "Run the agent in goal mode: the prompt becomes the objective, and the agent\n" +
			"auto-continues turns until the goal is complete, blocked, or budget-limited.\n" +
			"The agent can use create_goal, update_goal, and get_goal tools to manage its\n" +
			"own progress.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGoal(cmd, prompt, providerName, model, tokenBudget, turnBudget)
		},
	}
	cmd.Flags().StringVarP(&prompt, "prompt", "p", "", "goal objective to work towards")
	cmd.Flags().StringVar(&providerName, "provider", "", "provider config name")
	cmd.Flags().StringVar(&model, "model", "", "model id override")
	cmd.Flags().IntVar(&tokenBudget, "token-budget", 0, "maximum total tokens (0 = no limit)")
	cmd.Flags().IntVar(&turnBudget, "turn-budget", 0, "maximum agent turns (0 = no limit)")
	return cmd
}
