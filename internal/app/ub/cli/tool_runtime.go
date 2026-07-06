package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/feimingxliu/ub/internal/app/ub/agent"
	"github.com/feimingxliu/ub/internal/pkg/core/config"
	lspruntime "github.com/feimingxliu/ub/internal/pkg/integration/lsp"
	"github.com/feimingxliu/ub/internal/pkg/runtime/permission"
	"github.com/feimingxliu/ub/internal/pkg/tool"
	"github.com/feimingxliu/ub/internal/pkg/tool/fs"
	goaltool "github.com/feimingxliu/ub/internal/pkg/tool/goal"
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
	"github.com/feimingxliu/ub/internal/pkg/workspace/tooloutput"
)

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
	if err := goaltool.Register(reg); err != nil {
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
	if cfg != nil && cfg.Tools.Web.WebEnabled() {
		if err := webtool.Register(reg, webtool.Options{
			Enabled:             cfg.Tools.Web.WebEnabled(),
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
