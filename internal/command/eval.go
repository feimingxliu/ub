package command

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	agenteval "github.com/feimingxliu/ub/internal/eval"
	"github.com/spf13/cobra"
)

var runEvalTask = agenteval.Run

func newEvalCmd() *cobra.Command {
	var taskName string
	var providerName string
	var model string
	var timeout time.Duration
	var jsonOutput bool
	var keepWorkspace bool
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Run one isolated coding-agent evaluation task",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(taskName) == "" {
				return errors.New("eval task required: pass --task <name-or-path>")
			}
			workspace, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolve workspace: %w", err)
			}
			path, err := agenteval.ResolveTaskPath(workspace, taskName)
			if err != nil {
				return err
			}
			taskFile, err := agenteval.LoadTask(path)
			if err != nil {
				return err
			}
			report, runErr := runEvalTask(cmd.Context(), taskFile, agenteval.RunOptions{
				Provider:      providerName,
				Model:         model,
				Timeout:       timeout,
				KeepWorkspace: keepWorkspace,
			})
			if jsonOutput {
				err = agenteval.RenderJSON(cmd.OutOrStdout(), report)
			} else {
				err = agenteval.RenderText(cmd.OutOrStdout(), report)
			}
			if err != nil {
				return err
			}
			return runErr
		},
	}
	cmd.Flags().StringVar(&taskName, "task", "", "evaluation task name or YAML path")
	cmd.Flags().StringVar(&providerName, "provider", "", "provider config name")
	cmd.Flags().StringVar(&model, "model", "", "model id override")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "override task timeout")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output one JSON report")
	cmd.Flags().BoolVar(&keepWorkspace, "keep-workspace", false, "preserve the temporary workspace and state")
	return cmd
}
