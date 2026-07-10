// Package command wires the cobra command tree for the ub binary.
package command

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/feimingxliu/ub/internal/config"
	logx "github.com/feimingxliu/ub/internal/logx"
	"github.com/feimingxliu/ub/internal/maintenance"
	_ "github.com/feimingxliu/ub/internal/provider/anthropic"
	_ "github.com/feimingxliu/ub/internal/provider/compat"
	_ "github.com/feimingxliu/ub/internal/provider/fake"
	_ "github.com/feimingxliu/ub/internal/provider/openai"
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

// runWithFactory is the shared entry point for Execute and tests. It sets up
// logging (with rotation), runs the cobra command tree, and returns a process
// exit code. Panics from any command are recovered and rendered to stderr.
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
	root.AddCommand(newGoalCmd())
	root.AddCommand(newChatCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newPromptCmd())
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

// configForProcessStartup loads config early (before cobra parses flags) so
// that log rotation settings from config are available during logger setup.
// On error it falls back to defaults rather than failing — logging should
// always be available.
func configForProcessStartup(args []string) *config.Config {
	cfg, _, err := config.LoadWithOptions(preloadRootOptions(args))
	if err != nil {
		return config.Defaults()
	}
	return cfg
}

// preloadRootOptions extracts --profile and --dev from the raw args before
// cobra has parsed them. This is needed because config loading (and thus
// log rotation) happens before cobra command execution.
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

// logRotationOptions translates the cleanup config section into rotation
// parameters for the logger. Returns zero-value options when cleanup is
// disabled, so the logger runs without rotation.
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

// runStartupMaintenance runs periodic cleanup tasks (old sessions, stale
// logs, expired tool output spillover) at process start if enabled in config.
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

// loadConfigForCommand loads the merged effective config using flags from
// the cobra command tree (profile, dev, mode).
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

func notImplemented(iteration string) error {
	return fmt.Errorf("not implemented yet — scheduled for %s (see docs/roadmap.md)", iteration)
}
