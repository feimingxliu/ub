// Package cli wires the cobra command tree for the ub binary.
package cli

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/feimingxliu/ub/internal/config"
	logx "github.com/feimingxliu/ub/internal/log"
	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/provider"
	_ "github.com/feimingxliu/ub/internal/provider/anthropic"
	_ "github.com/feimingxliu/ub/internal/provider/fake"
	"github.com/feimingxliu/ub/internal/store"
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

	logger, cleanup, err := logx.SetupFromEnv(stderr)
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
	root := &cobra.Command{
		Use:           "ub",
		Short:         "ub — Ulimited Blade, a coding agent in your terminal",
		Long:          "ub is a terminal-based coding agent. Run `ub run` to start an interactive session.",
		Version:       Version(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate("{{.Version}}\n")

	root.AddCommand(newRunCmd())
	root.AddCommand(newChatCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newSessionsCmd())

	return root
}

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run an agent session (TUI by default)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented("I-22 (TUI) / I-21 (headless agent)")
		},
	}
}

func newChatCmd() *cobra.Command {
	var providerName string
	var model string

	cmd := &cobra.Command{
		Use:   "chat [prompt|-]",
		Short: "Send one prompt to a provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChat(cmd, args[0], providerName, model)
		},
	}
	cmd.Flags().StringVar(&providerName, "provider", "", "provider config name")
	cmd.Flags().StringVar(&model, "model", "", "model id override")
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
			cfg, _, err := config.Load()
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
			_, files, err := config.Load()
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

func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Manage agent sessions",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "ls",
		Short: "List sessions in the current workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsLS(cmd)
		},
	})
	return cmd
}

func runChat(cmd *cobra.Command, promptArg, providerFlag, modelFlag string) error {
	prompt, err := readChatPrompt(cmd, promptArg)
	if err != nil {
		return err
	}

	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	providerName, model, err := selectChatProvider(cfg, providerFlag, modelFlag)
	if err != nil {
		return err
	}
	providerCfg, ok := cfg.Providers[providerName]
	if !ok {
		return fmt.Errorf("provider %q not configured", providerName)
	}
	p, err := provider.New(providerName, providerCfg)
	if err != nil {
		return err
	}

	stream, err := p.Chat(cmd.Context(), provider.Request{
		Model:    model,
		Messages: []message.Message{message.Text(message.RoleUser, prompt)},
	})
	if err != nil {
		return err
	}
	defer stream.Close()

	for {
		event, err := stream.Next(cmd.Context())
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch event.Type {
		case provider.EventTextDelta:
			if _, err := io.WriteString(cmd.OutOrStdout(), event.Text); err != nil {
				return err
			}
		case provider.EventUsage:
			continue
		case provider.EventDone:
			return nil
		case provider.EventToolCall:
			if event.ToolName == "" {
				return fmt.Errorf("ub chat does not execute tool calls yet")
			}
			return fmt.Errorf("ub chat does not execute tool calls yet: received %q", event.ToolName)
		case provider.EventError:
			if event.Err != nil {
				return event.Err
			}
			return fmt.Errorf("provider returned error event")
		default:
			return fmt.Errorf("provider returned unsupported event type %q", event.Type)
		}
	}
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
	model := strings.TrimSpace(modelFlag)
	if model == "" && cfg != nil {
		model = strings.TrimSpace(cfg.DefaultModel)
	}
	if providerName == "" {
		providerName = providerFromModel(model)
	}
	if providerName == "" {
		return "", "", fmt.Errorf("provider required: set --provider or default_model as <provider>/<model>")
	}
	return providerName, model, nil
}

func providerFromModel(model string) string {
	providerName, _, ok := strings.Cut(model, "/")
	if !ok {
		return ""
	}
	return strings.TrimSpace(providerName)
}

func runSessionsLS(cmd *cobra.Command) error {
	path, err := store.DefaultPath()
	if err != nil {
		return fmt.Errorf("locate session store: %w", err)
	}
	st, err := store.Open(path)
	if err != nil {
		return err
	}
	defer st.Close()

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get cwd: %w", err)
	}
	sessions, err := st.ListSessions(cmd.Context(), cwd, 20)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "no sessions")
		return err
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "ID\tUPDATED\tTITLE\tMODEL"); err != nil {
		return err
	}
	for _, sess := range sessions {
		title := sess.Title
		if title == "" {
			title = "(untitled)"
		}
		model := sess.Model
		if model == "" {
			model = "-"
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			sess.ID,
			sess.UpdatedAt.Local().Format(time.RFC3339),
			title,
			model,
		); err != nil {
			return err
		}
	}
	return w.Flush()
}

func notImplemented(iteration string) error {
	return fmt.Errorf("not implemented yet — scheduled for %s (see docs/roadmap.md)", iteration)
}
