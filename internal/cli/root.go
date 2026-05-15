// Package cli wires the cobra command tree for the ub binary.
//
// I-01 only sets up the skeleton: a root command with --version and three
// placeholder subcommands (run / config / sessions). Real behavior lands in
// later iterations (see docs/roadmap.md).
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Execute runs the root command. It exits the process on failure.
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "ub",
		Short:         "ub — Ulimited Blade, a coding agent in your terminal",
		Long:          "ub is a terminal-based coding agent. Run `ub run` to start an interactive session.",
		Version:       Version(),
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.SetVersionTemplate("{{.Version}}\n")

	root.AddCommand(newRunCmd())
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

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or manage configuration",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print the merged effective configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented("I-02")
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "List configuration files used in the current invocation",
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented("I-02")
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
			return notImplemented("I-03")
		},
	})
	return cmd
}

func notImplemented(iteration string) error {
	return fmt.Errorf("not implemented yet — scheduled for %s (see docs/roadmap.md)", iteration)
}
