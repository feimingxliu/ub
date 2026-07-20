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

var (
	runEvalTask   = agenteval.Run
	runEvalMatrix = agenteval.RunMatrix
)

func newEvalCmd() *cobra.Command {
	var taskNames []string
	var targetValues []string
	var providerName string
	var model string
	var repeat int
	var parallel int
	var timeout time.Duration
	var jsonOutput bool
	var keepWorkspace bool
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Run isolated coding-agent evaluation tasks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(taskNames) == 0 {
				return errors.New("eval task required: pass --task <name-or-path>")
			}
			if len(targetValues) > 0 && (strings.TrimSpace(providerName) != "" || strings.TrimSpace(model) != "") {
				return errors.New("--target cannot be combined with --provider or --model")
			}
			if repeat <= 0 {
				return errors.New("--repeat must be positive")
			}
			if parallel <= 0 || parallel > agenteval.MaxMatrixParallel {
				return fmt.Errorf("--parallel must be between 1 and %d", agenteval.MaxMatrixParallel)
			}
			workspace, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolve workspace: %w", err)
			}
			taskFiles := make([]agenteval.TaskFile, 0, len(taskNames))
			for _, taskName := range taskNames {
				path, err := agenteval.ResolveTaskPath(workspace, taskName)
				if err != nil {
					return err
				}
				taskFile, err := agenteval.LoadTask(path)
				if err != nil {
					return err
				}
				taskFiles = append(taskFiles, taskFile)
			}
			options := agenteval.RunOptions{
				Provider:      providerName,
				Model:         model,
				Timeout:       timeout,
				KeepWorkspace: keepWorkspace,
			}
			matrixMode := len(taskFiles) > 1 || len(targetValues) > 0 || repeat > 1 || parallel > 1
			if !matrixMode {
				report, runErr := runEvalTask(cmd.Context(), taskFiles[0], options)
				if jsonOutput {
					err = agenteval.RenderJSON(cmd.OutOrStdout(), report)
				} else {
					err = agenteval.RenderText(cmd.OutOrStdout(), report)
				}
				if err != nil {
					return err
				}
				return runErr
			}
			targets, err := evalTargets(targetValues, providerName, model)
			if err != nil {
				return err
			}
			matrix, runErr := runEvalMatrix(cmd.Context(), agenteval.MatrixRequest{
				Tasks: taskFiles, Targets: targets, Repeat: repeat, Parallel: parallel, Options: options,
			})
			if jsonOutput {
				err = agenteval.RenderMatrixJSON(cmd.OutOrStdout(), matrix)
			} else {
				err = agenteval.RenderMatrixText(cmd.OutOrStdout(), matrix)
			}
			if err != nil {
				return err
			}
			return runErr
		},
	}
	cmd.Flags().StringArrayVar(&taskNames, "task", nil, "evaluation task name or YAML path (repeatable)")
	cmd.Flags().StringArrayVar(&targetValues, "target", nil, "provider=model target (repeatable)")
	cmd.Flags().StringVar(&providerName, "provider", "", "provider config name")
	cmd.Flags().StringVar(&model, "model", "", "model id override")
	cmd.Flags().IntVar(&repeat, "repeat", 1, "number of samples per task and target")
	cmd.Flags().IntVar(&parallel, "parallel", 1, "maximum concurrent matrix samples")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "override task timeout")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output one JSON report")
	cmd.Flags().BoolVar(&keepWorkspace, "keep-workspace", false, "preserve the temporary workspace and state")
	return cmd
}

func evalTargets(values []string, provider, model string) ([]agenteval.Target, error) {
	if len(values) == 0 {
		return []agenteval.Target{{Provider: strings.TrimSpace(provider), Model: strings.TrimSpace(model)}}, nil
	}
	targets := make([]agenteval.Target, 0, len(values))
	for _, value := range values {
		providerValue, modelValue, ok := strings.Cut(value, "=")
		providerValue = strings.TrimSpace(providerValue)
		modelValue = strings.TrimSpace(modelValue)
		if !ok || providerValue == "" || modelValue == "" {
			return nil, fmt.Errorf("invalid --target %q (want provider=model)", value)
		}
		targets = append(targets, agenteval.Target{Provider: providerValue, Model: modelValue})
	}
	return targets, nil
}
