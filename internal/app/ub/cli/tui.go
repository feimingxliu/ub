package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/feimingxliu/ub/internal/app/ub/agent"
	"github.com/feimingxliu/ub/internal/app/ub/tui"
	"github.com/feimingxliu/ub/internal/pkg/core/config"
	"github.com/feimingxliu/ub/internal/pkg/core/execution"
	"github.com/feimingxliu/ub/internal/pkg/core/reasoning"
	"github.com/feimingxliu/ub/internal/pkg/llm/modelinfo"
	"github.com/feimingxliu/ub/internal/pkg/llm/provider"
	logx "github.com/feimingxliu/ub/internal/pkg/runtime/log"
	"github.com/feimingxliu/ub/internal/pkg/runtime/permission"
	"github.com/spf13/cobra"
	"golang.org/x/sync/singleflight"
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
		go runStartupMaintenance(cmd, cfg)
	}

	permBridge := tui.NewPermissionBridge()
	askBridge := tui.NewAskBridge()
	planModeBridge := tui.NewPlanModeBridge()
	limitBridge := tui.NewLimitBridge()
	backgroundEvents := make(chan tui.Event, 64)
	runner, err := newTUIAgentRunner(cmd, cfg, permBridge, providerFlag, modelFlag, backgroundEvents)
	if err != nil {
		return err
	}
	runner.asker = askBridge
	runner.planMode = planModeBridge
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
		Asks:             askBridge.Requests(),
		PlanModes:        planModeBridge.Requests(),
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
	providerCache        *providerCache
	providerName         string
	providerCfg          config.ProviderConfig
	model                string
	modelInfo            modelinfo.Info
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
	startupMode          execution.Mode
	prePlanMode          execution.Mode
	modeMu               sync.RWMutex
	eventTimeout         time.Duration
	permission           *permission.Manager
	state                *chatSessionState
	closedStore          bool
	maxTurns             int
	limitAsker           agent.LimitAsker
	asker                agent.Asker
	planMode             agent.PlanModeController
	// injectCh feeds user guidance text into the currently running agent
	// loop. It is created once when the runner is built and reused across
	// runs: the agent loop drains it between tool iterations and flushes any
	// remainder at the end of each Run, so it starts each run empty. Building
	// it once (rather than per-run) avoids a data race between Inject (TUI
	// goroutine) and newAgent (run goroutine) over the field, and lets Inject
	// buffer guidance even in the window before a run's agent loop starts.
	injectCh           chan string
	providerCheckMu    sync.Mutex
	providerChecks     map[string]providerCheck
	providerCheckGroup singleflight.Group

	// cachedMessages holds the reconstructed InitialMessages for the loaded
	// session. Populated lazily by Messages() so we only scan the rollout once
	// per session-load instead of on every sessionState() call.
	cachedMessages        []tui.InitialMessage
	cachedMessagesSession string
}

func newTUIAgentRunner(cmd *cobra.Command, cfg *config.Config, asker permission.Asker, providerFlag, modelFlag string, backgroundEvents chan<- tui.Event) (*tuiAgentRunner, error) {
	mainRole, err := resolveMainModelRole(cmd.Context(), cfg, providerFlag, modelFlag)
	if err != nil {
		return nil, err
	}
	providerName := mainRole.ProviderName
	providerCfg := mainRole.ProviderConfig
	model := mainRole.Model
	providers := newProviderCache()
	p, err := providers.Get(providerName, providerCfg)
	if err != nil {
		return nil, fmt.Errorf("create provider %q: %w", providerName, err)
	}
	models := mergeModelCandidates(
		model,
		configuredProviderModels(providerCfg, ""),
	)
	mode, err := execution.ParseMode(cfg.ExecutionMode)
	if err != nil {
		return nil, err
	}
	approvalSetup, err := newApprovalAgentSetupForStartup(cmd.Context(), cfg, providerName, model, providers)
	if err != nil {
		return nil, err
	}
	summarySetup, err := newSummarySetup(cmd.Context(), cfg, providerName, providerCfg, model, providers)
	if err != nil {
		return nil, err
	}
	autoMemorySetup, err := newAutoMemorySetupForStartup(cmd.Context(), cfg, providerName, providerCfg, model, providers)
	if err != nil {
		return nil, err
	}
	smallModels := mergeModelCandidates(autoMemorySetup.Model, []string{model}, models)
	tools, err := newToolRuntime(cmd.Context(), cfg)
	if err != nil {
		return nil, err
	}
	closeTools := true
	defer func() {
		if closeTools {
			_ = tools.Close()
		}
	}()
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
	runner := &tuiAgentRunner{
		cmd:                  cmd,
		cfg:                  cfg,
		provider:             p,
		providerCache:        providers,
		providerName:         providerName,
		providerCfg:          providerCfg,
		model:                model,
		modelInfo:            mainRole.Info,
		models:               models,
		reasoningPref:        cfg.Reasoning,
		reasoning:            mainRole.cloneReasoning(),
		efforts:              mainRole.Efforts,
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
		startupMode:          mode,
		eventTimeout:         effectiveTUIEventTimeout(providerCfg.Timeout),
		permission:           perm,
		maxTurns:             cfg.MaxTurns,
		providerChecks:       map[string]providerCheck{},
		injectCh:             make(chan string, 16),
	}
	closeTools = false
	return runner, nil
}

func effectiveTUIEventTimeout(timeout time.Duration) time.Duration {
	// Provider/tool timeouts are enforced in their own layers. The TUI event
	// waiter must not turn normal waits for human approval or long-running
	// tools into a synthetic fatal error.
	return 0
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
