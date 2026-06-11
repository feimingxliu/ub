// Package cli wires the cobra command tree for the ub binary.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/feimingxliu/ub/internal/app/ub/agent"
	"github.com/feimingxliu/ub/internal/pkg/core/config"
	"github.com/feimingxliu/ub/internal/pkg/core/execution"
	"github.com/feimingxliu/ub/internal/pkg/core/message"
	lspruntime "github.com/feimingxliu/ub/internal/pkg/integration/lsp"
	"github.com/feimingxliu/ub/internal/pkg/llm/provider"
	_ "github.com/feimingxliu/ub/internal/pkg/llm/provider/anthropic"
	_ "github.com/feimingxliu/ub/internal/pkg/llm/provider/compat"
	_ "github.com/feimingxliu/ub/internal/pkg/llm/provider/fake"
	_ "github.com/feimingxliu/ub/internal/pkg/llm/provider/openai"
	"github.com/feimingxliu/ub/internal/pkg/runtime/hook"
	logx "github.com/feimingxliu/ub/internal/pkg/runtime/log"
	"github.com/feimingxliu/ub/internal/pkg/runtime/maintenance"
	"github.com/feimingxliu/ub/internal/pkg/runtime/permission"
	"github.com/feimingxliu/ub/internal/pkg/tool"
	"github.com/feimingxliu/ub/internal/pkg/tool/fs"
	"github.com/feimingxliu/ub/internal/pkg/tool/job"
	lsptool "github.com/feimingxliu/ub/internal/pkg/tool/lsp"
	mcptool "github.com/feimingxliu/ub/internal/pkg/tool/mcp"
	memorytool "github.com/feimingxliu/ub/internal/pkg/tool/memory"
	"github.com/feimingxliu/ub/internal/pkg/tool/plan"
	"github.com/feimingxliu/ub/internal/pkg/tool/search"
	"github.com/feimingxliu/ub/internal/pkg/tool/shell"
	tasktool "github.com/feimingxliu/ub/internal/pkg/tool/task"
	todotool "github.com/feimingxliu/ub/internal/pkg/tool/todo"
	webtool "github.com/feimingxliu/ub/internal/pkg/tool/web"
	"github.com/feimingxliu/ub/internal/pkg/workspace/rollout"
	"github.com/feimingxliu/ub/internal/pkg/workspace/store"
	"github.com/feimingxliu/ub/internal/pkg/workspace/tooloutput"
	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"
)

// Execute runs the root command. It exits the process on failure.
func Execute() {
	os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr))
}

// Run executes the CLI with injected streams and returns a process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	return runWithFactory(args, stdout, stderr, newRootCmd)
}

func runWithFactory(args []string, stdout, stderr io.Writer, cmdFactory func() *cobra.Command) (code int) {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(stderr, "panic: %v\n%s", r, debug.Stack())
			code = 1
		}
	}()

	startupCfg := configForProcessStartup(args)
	logger, cleanup, err := logx.SetupFromEnvWithRotation(stderr, logRotationOptions(startupCfg))
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	defer func() {
		if cleanup == nil {
			return
		}
		if err := cleanup(); err != nil && code == 0 {
			fmt.Fprintf(stderr, "error: close log: %v\n", err)
			code = 1
		}
	}()

	logger.Debug("cli command start", "args", args)
	cmd := cmdFactory()
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	var opts runtimeOptions
	root := &cobra.Command{
		Use:           "ub",
		Short:         "ub — Ulimited Blade, a coding agent in your terminal",
		Long:          "ub is a terminal-based coding agent. Run `ub` to open the TUI or use an explicit subcommand for headless workflows.",
		Version:       Version(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if opts.resume == resumeSelectSentinel && len(args) <= 1 {
				return nil
			}
			if len(args) == 0 {
				return nil
			}
			return fmt.Errorf("unknown command %q", args[0])
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfigForCommand(cmd)
			if err != nil {
				return err
			}
			resume := opts.resume
			if resume == resumeSelectSentinel && len(args) == 1 {
				resume = args[0]
			}
			return runTUI(cmd, cfg, resume, opts.provider, opts.model)
		},
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.PersistentFlags().StringVar(&opts.profile, "profile", "", "configuration profile to apply")
	root.PersistentFlags().BoolVar(&opts.dev, "dev", false, "use the dev profile")
	root.PersistentFlags().StringVar(&opts.mode, "mode", "", "execution mode: work, plan, or auto")
	root.Flags().StringVar(&opts.provider, "provider", "", "provider config name for TUI")
	root.Flags().StringVar(&opts.model, "model", "", "model id override for TUI")
	root.Flags().StringVar(&opts.resume, "resume", "", "choose a TUI session to resume, or resume the specified id with --resume=<id>")
	root.Flags().Lookup("resume").NoOptDefVal = resumeSelectSentinel

	root.AddCommand(newRunCmd())
	root.AddCommand(newChatCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newRolloutCmd())
	root.AddCommand(newSessionsCmd())

	return root
}

type runtimeOptions struct {
	profile  string
	dev      bool
	mode     string
	resume   string
	provider string
	model    string
}

func configForProcessStartup(args []string) *config.Config {
	cfg, _, err := config.LoadWithOptions(preloadRootOptions(args))
	if err != nil {
		return config.Defaults()
	}
	return cfg
}

func preloadRootOptions(args []string) config.LoadOptions {
	var opts config.LoadOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--profile" && i+1 < len(args):
			opts.Profile = args[i+1]
			i++
		case strings.HasPrefix(arg, "--profile="):
			opts.Profile = strings.TrimPrefix(arg, "--profile=")
		case arg == "--dev":
			opts.Dev = true
		case strings.HasPrefix(arg, "--dev="):
			if v, err := strconv.ParseBool(strings.TrimPrefix(arg, "--dev=")); err == nil {
				opts.Dev = v
			}
		}
	}
	return opts
}

func logRotationOptions(cfg *config.Config) logx.RotationOptions {
	if cfg == nil {
		cfg = config.Defaults()
	}
	if !cfg.Cleanup.CleanupEnabled() {
		return logx.RotationOptions{}
	}
	maxSizeBytes := int64(cfg.Cleanup.Logs.MaxSizeMB) * 1024 * 1024
	return logx.RotationOptions{
		MaxSizeBytes: maxSizeBytes,
		MaxBackups:   cfg.Cleanup.Logs.MaxBackups,
	}
}

func runStartupMaintenance(cmd *cobra.Command, cfg *config.Config) {
	maintenance.RunStartup(cmd.Context(), cfg)
}

func newRunCmd() *cobra.Command {
	var prompt string
	var providerName string
	var model string

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a headless agent session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgent(cmd, prompt, providerName, model)
		},
	}
	cmd.Flags().StringVarP(&prompt, "prompt", "p", "", "prompt to send to the agent")
	cmd.Flags().StringVar(&providerName, "provider", "", "provider config name")
	cmd.Flags().StringVar(&model, "model", "", "model id override")
	return cmd
}

func newChatCmd() *cobra.Command {
	var providerName string
	var model string
	var sessionID string
	var forceNew bool

	cmd := &cobra.Command{
		Use:   "chat [prompt|-]",
		Short: "Send one prompt to a provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChat(cmd, args[0], providerName, model, chatOptions{
				SessionID: sessionID,
				New:       forceNew,
			})
		},
	}
	cmd.Flags().StringVar(&providerName, "provider", "", "provider config name")
	cmd.Flags().StringVar(&model, "model", "", "model id override")
	cmd.Flags().StringVar(&sessionID, "session", "", "continue an existing session id")
	cmd.Flags().BoolVar(&forceNew, "new", false, "force creation of a new session")
	return cmd
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or manage configuration",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print the merged effective configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadConfigForCommand(cmd)
			if err != nil {
				return err
			}
			redacted := config.Redact(cfg)
			out, err := yaml.Marshal(redacted)
			if err != nil {
				return fmt.Errorf("marshal config: %w", err)
			}
			_, err = cmd.OutOrStdout().Write(out)
			return err
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "List configuration files used in the current invocation",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, files, err := loadConfigForCommand(cmd)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "(no config files loaded; using built-in defaults)")
				return err
			}
			for _, file := range files {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), file); err != nil {
					return err
				}
			}
			return nil
		},
	})
	return cmd
}

func loadConfigForCommand(cmd *cobra.Command) (*config.Config, []string, error) {
	opts, err := loadOptionsForCommand(cmd)
	if err != nil {
		return nil, nil, err
	}
	return config.LoadWithOptions(opts)
}

func loadOptionsForCommand(cmd *cobra.Command) (config.LoadOptions, error) {
	root := cmd.Root()
	profile, err := root.PersistentFlags().GetString("profile")
	if err != nil {
		return config.LoadOptions{}, err
	}
	dev, err := root.PersistentFlags().GetBool("dev")
	if err != nil {
		return config.LoadOptions{}, err
	}
	mode, err := root.PersistentFlags().GetString("mode")
	if err != nil {
		return config.LoadOptions{}, err
	}
	return config.LoadOptions{
		Profile:       profile,
		Dev:           dev,
		ExecutionMode: mode,
	}, nil
}

func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Manage agent sessions",
	}
	var all bool
	lsCmd := &cobra.Command{
		Use:   "ls",
		Short: "List sessions in the current workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsLS(cmd, all)
		},
	}
	lsCmd.Flags().BoolVar(&all, "all", false, "list sessions across all workspaces")
	cmd.AddCommand(lsCmd)
	var (
		searchLimit     int
		searchWorkspace string
	)
	searchCmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search rollout text across all sessions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsSearch(cmd, args[0], searchLimit, searchWorkspace)
		},
	}
	searchCmd.Flags().IntVar(&searchLimit, "limit", 200, "max matches to return (0 = unlimited)")
	searchCmd.Flags().StringVar(&searchWorkspace, "workspace", "", "restrict to a single workspace path")
	cmd.AddCommand(searchCmd)
	cmd.AddCommand(&cobra.Command{
		Use:     "rm <session-id> [session-id...]",
		Aliases: []string{"delete", "del"},
		Short:   "Delete sessions by id",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsRM(cmd, args)
		},
	})
	var (
		yes      bool
		clearAll bool
	)
	clearCmd := &cobra.Command{
		Use:   "clear",
		Short: "Delete all sessions in the current workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsClear(cmd, yes, clearAll)
		},
	}
	clearCmd.Flags().BoolVar(&yes, "yes", false, "confirm deletion")
	clearCmd.Flags().BoolVar(&clearAll, "all", false, "clear sessions across all workspaces")
	cmd.AddCommand(clearCmd)
	return cmd
}

type chatOptions struct {
	SessionID string
	New       bool
}

type chatSessionState struct {
	store          *store.Store
	rollout        *rollout.SQLite
	session        store.Session
	history        []message.Message
	contextHistory []message.Message
	nextTurn       int
	sessionID      string
}

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
	providerName, model, err := selectChatProvider(cfg, providerFlag, modelFlag)
	if err != nil {
		return err
	}
	providerCfg, ok := cfg.Providers[providerName]
	if !ok {
		return fmt.Errorf("provider %q not configured; check `ub config show`", providerName)
	}
	model, err = selectProviderModel(cmd.Context(), providerName, providerCfg, model)
	if err != nil {
		return err
	}
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
	defer state.store.Close()
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
		Reasoning:           chatReasoningConfig(cfg, providerName, providerCfg, model),
		MaxContextTokens:    chatMaxContextTokens(providerName, providerCfg, model),
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
		reasoningCfg:     chatReasoningConfig(cfg, providerName, providerCfg, model),
		maxContextTokens: chatMaxContextTokens(providerName, providerCfg, model),
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

type toolRuntime struct {
	Registry       *tool.Registry
	Workspace      string
	Warnings       []error
	MCPConnections *mcptool.Connections
	close          func() error
}

func (r *toolRuntime) Close() error {
	if r == nil || r.close == nil {
		return nil
	}
	return r.close()
}

func newToolRuntime(ctx context.Context, cfg *config.Config) (*toolRuntime, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get cwd: %w", err)
	}
	reg := tool.New()
	var warnings []error
	var closers []func() error
	var lspManager *lspruntime.LazyManager
	if cfg != nil && len(cfg.LSPServers) > 0 {
		lspManager = lspruntime.NewLazyManager(cwd, cfg.LSPServers)
		if lspManager != nil {
			closers = append(closers, lspManager.Close)
		}
	}
	limits := tooloutput.EffectiveLimits(config.ContextConfig{})
	if cfg != nil {
		limits = tooloutput.EffectiveLimits(cfg.Context)
	}
	stateRoot, err := tooloutput.StateRoot()
	if err != nil && strings.TrimSpace(limits.SpilloverDir) == "" {
		warnings = append(warnings, fmt.Errorf("locate tool-output state: %w", err))
	}
	readStateRoot := ""
	if err == nil || strings.TrimSpace(limits.SpilloverDir) != "" {
		if outputRoot, err := tooloutput.OutputRootForLimits(limits, stateRoot); err == nil {
			readStateRoot = outputRoot
		} else {
			warnings = append(warnings, fmt.Errorf("locate tool-output root: %w", err))
		}
	}
	if err := fs.RegisterWithOptions(reg, cwd, fs.Options{
		StateRoot:      readStateRoot,
		OutputRoot:     readStateRoot,
		ReadMaxLines:   limits.InlineMaxLines,
		ChangeNotifier: lspManager,
	}); err != nil {
		return nil, err
	}
	if err := reg.Register(agent.NewAskTool()); err != nil {
		return nil, err
	}
	for _, planModeTool := range agent.NewPlanModeTools() {
		if err := reg.Register(planModeTool); err != nil {
			return nil, err
		}
	}
	if err := lsptool.Register(reg, lspManager); err != nil {
		return nil, err
	}
	if err := tasktool.Register(reg); err != nil {
		return nil, err
	}
	if err := todotool.Register(reg); err != nil {
		return nil, err
	}
	for _, register := range []func(*tool.Registry, string) error{
		search.Register,
		shell.Register,
		plan.Register,
		memorytool.Register,
	} {
		if err := register(reg, cwd); err != nil {
			return nil, err
		}
	}
	if cfg != nil && cfg.Tools.Web.Enabled {
		if err := webtool.Register(reg, webtool.Options{
			Enabled:             cfg.Tools.Web.Enabled,
			Provider:            cfg.Tools.Web.Provider,
			APIKey:              cfg.Tools.Web.APIKey,
			BaseURL:             cfg.Tools.Web.BaseURL,
			UserAgent:           cfg.Tools.Web.UserAgent,
			Timeout:             cfg.Tools.Web.Timeout,
			MaxFetchBytes:       cfg.Tools.Web.MaxFetchBytes,
			AllowDomains:        cfg.Tools.Web.AllowDomains,
			DenyDomains:         cfg.Tools.Web.DenyDomains,
			AllowPrivateNetwork: cfg.Tools.Web.AllowPrivateNetwork,
		}); err != nil {
			return nil, err
		}
	}
	jobCfg := config.Defaults().Tools.Job
	if cfg != nil {
		if cfg.Tools.Job.MaxConcurrent != 0 {
			jobCfg.MaxConcurrent = cfg.Tools.Job.MaxConcurrent
		}
		if cfg.Tools.Job.Retention != 0 {
			jobCfg.Retention = cfg.Tools.Job.Retention
		}
		if cfg.Tools.Job.CleanupInterval != 0 {
			jobCfg.CleanupInterval = cfg.Tools.Job.CleanupInterval
		}
	}
	jobMgr, err := job.RegisterWithOptions(reg, cwd, job.ManagerOptions{
		MaxConcurrent:   jobCfg.MaxConcurrent,
		Retention:       jobCfg.Retention,
		CleanupInterval: jobCfg.CleanupInterval,
	})
	if err != nil {
		return nil, err
	}
	closers = append(closers, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return jobMgr.Shutdown(ctx)
	})
	runtime := &toolRuntime{
		Registry:  reg,
		Workspace: cwd,
		Warnings:  warnings,
		close: func() error {
			var err error
			for i := len(closers) - 1; i >= 0; i-- {
				if closeErr := closers[i](); closeErr != nil && err == nil {
					err = closeErr
				}
			}
			return err
		},
	}
	if cfg != nil {
		conns, warnings := mcptool.RegisterConfigured(ctx, reg, cfg.MCPServers)
		closers = append(closers, conns.Close)
		runtime.MCPConnections = conns
		runtime.Warnings = append(runtime.Warnings, warnings...)
	}
	return runtime, nil
}

func writeToolWarnings(w io.Writer, warnings []error) {
	for _, warning := range warnings {
		if warning != nil {
			fmt.Fprintf(w, "warning: %v\n", warning)
		}
	}
}

type autoAllowAsker struct{}

func (autoAllowAsker) Ask(context.Context, permission.Request) (permission.Decision, error) {
	return permission.DecisionAllow, nil
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
	providerName, model, err := selectChatProvider(cfg, providerFlag, modelFlag)
	if err != nil {
		return err
	}
	providerCfg, ok := cfg.Providers[providerName]
	if !ok {
		return fmt.Errorf("provider %q not configured; check `ub config show`", providerName)
	}
	model, err = selectProviderModel(cmd.Context(), providerName, providerCfg, model)
	if err != nil {
		return err
	}
	providers := newProviderCache()
	p, err := providers.Get(providerName, providerCfg)
	if err != nil {
		return fmt.Errorf("create provider %q: %w", providerName, err)
	}

	state, err := startChatRollout(cmd, prompt, providerName, model, opts)
	if err != nil {
		return err
	}
	defer state.store.Close()

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
		Reasoning: chatReasoningConfig(cfg, providerName, providerCfg, model),
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
	cwd, err := currentWorkspace()
	if err != nil {
		_ = st.Close()
		return nil, err
	}

	if sessionID := strings.TrimSpace(opts.SessionID); sessionID != "" {
		sess, err := st.GetSession(cmd.Context(), sessionID)
		if errors.Is(err, store.ErrNotFound) {
			_ = st.Close()
			return nil, fmt.Errorf("session %q not found", sessionID)
		}
		if err != nil {
			_ = st.Close()
			return nil, err
		}
		history, contextHistory, nextTurn, err := readChatHistory(cmd, ro, sessionID)
		if err != nil {
			_ = st.Close()
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
		_ = st.Close()
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

func runSessionsLS(cmd *cobra.Command, all bool) error {
	path, err := store.DefaultPath()
	if err != nil {
		return fmt.Errorf("locate session store: %w", err)
	}
	st, err := store.Open(path)
	if err != nil {
		return err
	}
	defer st.Close()

	if all {
		sessions, err := st.ListAllSessions(cmd.Context())
		if err != nil {
			return err
		}
		return printAllSessions(cmd.OutOrStdout(), sessions)
	}

	cwd, err := currentWorkspace()
	if err != nil {
		return err
	}
	sessions, err := st.ListSessions(cmd.Context(), cwd, 20)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "no sessions")
		return err
	}

	return printSessionTable(cmd.OutOrStdout(), sessions)
}

func printAllSessions(out io.Writer, sessions []store.Session) error {
	if len(sessions) == 0 {
		_, err := fmt.Fprintln(out, "no sessions")
		return err
	}
	currentWorkspace := ""
	for i, sess := range sessions {
		if sess.Workspace == currentWorkspace {
			continue
		}
		if currentWorkspace != "" {
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
		currentWorkspace = sess.Workspace
		if _, err := fmt.Fprintf(out, "WORKSPACE %s\n", currentWorkspace); err != nil {
			return err
		}
		groupEnd := i + 1
		for groupEnd < len(sessions) && sessions[groupEnd].Workspace == currentWorkspace {
			groupEnd++
		}
		if err := printSessionTable(out, sessions[i:groupEnd]); err != nil {
			return err
		}
	}
	return nil
}

func printSessionTable(out io.Writer, sessions []store.Session) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "ID\tUPDATED\tTITLE\tPROVIDER\tMODEL"); err != nil {
		return err
	}
	for _, sess := range sessions {
		title := sess.Title
		if title == "" {
			title = "(untitled)"
		}
		provider := sess.Provider
		if provider == "" {
			provider = "-"
		}
		model := sess.Model
		if model == "" {
			model = "-"
		}
		if _, err := fmt.Fprintf(
			w, "%s\t%s\t%s\t%s\t%s\n",
			sess.ID,
			sess.UpdatedAt.Local().Format(time.RFC3339),
			title,
			provider,
			model,
		); err != nil {
			return err
		}
	}
	return w.Flush()
}

type sessionSearchMatch struct {
	Session   store.Session
	Turn      int
	Type      rollout.Type
	Time      time.Time
	Snippet   string
	Workspace string
}

func runSessionsSearch(cmd *cobra.Command, query string, limit int, workspace string) error {
	query = strings.TrimSpace(query)
	if query == "" {
		return fmt.Errorf("search query is empty")
	}
	path, err := store.DefaultPath()
	if err != nil {
		return fmt.Errorf("locate session store: %w", err)
	}
	st, err := store.Open(path)
	if err != nil {
		return err
	}
	defer st.Close()

	sessions, err := st.ListAllSessions(cmd.Context())
	if err != nil {
		return err
	}
	if workspace = strings.TrimSpace(workspace); workspace != "" {
		canonical, err := canonicalWorkspace(workspace)
		if err != nil {
			return err
		}
		filtered := sessions[:0]
		for _, sess := range sessions {
			if sess.Workspace == canonical {
				filtered = append(filtered, sess)
			}
		}
		sessions = filtered
	}
	ro, err := rollout.New(st)
	if err != nil {
		return err
	}
	matches, err := searchSessions(cmd.Context(), ro, sessions, query, limit)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "no matches")
		return err
	}
	return printSessionSearchMatches(cmd.OutOrStdout(), matches)
}

// errSearchLimitReached short-circuits ForEach once the caller has accumulated
// the requested number of matches; the surrounding loop swallows it.
var errSearchLimitReached = fmt.Errorf("search: match limit reached")

func searchSessions(ctx context.Context, reader rollout.Reader, sessions []store.Session, query string, limit int) ([]sessionSearchMatch, error) {
	needle := strings.ToLower(strings.TrimSpace(query))
	var matches []sessionSearchMatch
	for _, sess := range sessions {
		err := reader.ForEach(ctx, sess.ID, func(event rollout.Event) error {
			text, err := rolloutEventSearchText(event)
			if err != nil {
				return err
			}
			if !strings.Contains(strings.ToLower(text), needle) {
				return nil
			}
			matches = append(matches, sessionSearchMatch{
				Session:   sess,
				Turn:      event.Turn,
				Type:      event.Type,
				Time:      event.Time,
				Snippet:   searchSnippet(text, query, 120),
				Workspace: sess.Workspace,
			})
			if limit > 0 && len(matches) >= limit {
				return errSearchLimitReached
			}
			return nil
		})
		if errors.Is(err, errSearchLimitReached) {
			return matches, nil
		}
		if err != nil {
			return nil, err
		}
	}
	return matches, nil
}

func printSessionSearchMatches(out io.Writer, matches []sessionSearchMatch) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "WORKSPACE\tSESSION\tTURN\tTYPE\tTIME\tTITLE\tMATCH"); err != nil {
		return err
	}
	for _, match := range matches {
		title := match.Session.Title
		if title == "" {
			title = "(untitled)"
		}
		if _, err := fmt.Fprintf(
			w,
			"%s\t%s\t%d\t%s\t%s\t%s\t%s\n",
			match.Workspace,
			match.Session.ID,
			match.Turn,
			match.Type,
			match.Time.Local().Format(time.RFC3339),
			title,
			match.Snippet,
		); err != nil {
			return err
		}
	}
	return w.Flush()
}

func rolloutEventSearchText(event rollout.Event) (string, error) {
	if msg, ok, err := rollout.MessageFromEvent(event); err != nil {
		return "", err
	} else if ok {
		text := msg.Text()
		if event.Type == rollout.TypeToolResult {
			var payload rollout.ToolResultPayload
			if err := json.Unmarshal(event.Payload, &payload); err == nil && len(payload.Metadata) > 0 {
				var metadata []string
				for key, value := range payload.Metadata {
					metadata = append(metadata, key+"="+value)
				}
				sort.Strings(metadata)
				text = strings.TrimSpace(text + " " + strings.Join(metadata, " "))
			}
		}
		return text, nil
	}
	switch event.Type {
	case rollout.TypeError:
		var payload rollout.ErrorPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return "", fmt.Errorf("decode rollout error event %s: %w", event.ID, err)
		}
		return payload.Message, nil
	case rollout.TypeActivity:
		var payload rollout.ActivityPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return "", fmt.Errorf("decode rollout activity event %s: %w", event.ID, err)
		}
		return strings.Join([]string{payload.ActivityKind, payload.ToolName, payload.Status, payload.Summary, payload.Content, payload.Decision, payload.Source, payload.Reason}, " "), nil
	case rollout.TypeMemoryWrite:
		var payload rollout.MemoryWritePayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return "", fmt.Errorf("decode rollout memory_write event %s: %w", event.ID, err)
		}
		return strings.Join([]string{payload.Scope, payload.Category, payload.Text, payload.Path, payload.Source, payload.Action}, " "), nil
	default:
		return "", nil
	}
}

func searchSnippet(text, query string, maxRunes int) string {
	text = strings.Join(strings.Fields(text), " ")
	if maxRunes <= 0 || len([]rune(text)) <= maxRunes {
		return text
	}
	lowerText := strings.ToLower(text)
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	idx := strings.Index(lowerText, lowerQuery)
	if idx < 0 {
		return trimRunes(text, maxRunes)
	}
	runeStart := utf8.RuneCountInString(text[:idx])
	queryRunes := utf8.RuneCountInString(lowerQuery)
	start := max(0, runeStart-(maxRunes-queryRunes)/2)
	return trimRunesFrom(text, start, maxRunes)
}

func trimRunes(text string, maxRunes int) string {
	return trimRunesFrom(text, 0, maxRunes)
}

func trimRunesFrom(text string, start, maxRunes int) string {
	runes := []rune(text)
	if start > len(runes) {
		start = len(runes)
	}
	end := min(len(runes), start+maxRunes)
	prefix := ""
	if start > 0 {
		prefix = "..."
	}
	suffix := ""
	if end < len(runes) {
		suffix = "..."
	}
	return prefix + string(runes[start:end]) + suffix
}

func runSessionsRM(cmd *cobra.Command, ids []string) error {
	path, err := store.DefaultPath()
	if err != nil {
		return fmt.Errorf("locate session store: %w", err)
	}
	st, err := store.Open(path)
	if err != nil {
		return err
	}
	defer st.Close()

	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return fmt.Errorf("session id is empty")
		}
		if err := st.DeleteSession(cmd.Context(), id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("session %q not found", id)
			}
			return err
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "deleted %s\n", id); err != nil {
			return err
		}
	}
	return nil
}

func runSessionsClear(cmd *cobra.Command, yes, all bool) error {
	if !yes {
		return fmt.Errorf("refusing to delete sessions without --yes")
	}
	path, err := store.DefaultPath()
	if err != nil {
		return fmt.Errorf("locate session store: %w", err)
	}
	st, err := store.Open(path)
	if err != nil {
		return err
	}
	defer st.Close()

	var deleted int64
	if all {
		deleted, err = st.DeleteAllSessions(cmd.Context())
	} else {
		cwd, werr := currentWorkspace()
		if werr != nil {
			return werr
		}
		deleted, err = st.DeleteWorkspaceSessions(cmd.Context(), cwd)
	}
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "deleted %d sessions\n", deleted); err != nil {
		return err
	}
	return nil
}

func notImplemented(iteration string) error {
	return fmt.Errorf("not implemented yet — scheduled for %s (see docs/roadmap.md)", iteration)
}
