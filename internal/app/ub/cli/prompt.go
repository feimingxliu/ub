package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/feimingxliu/ub/internal/app/ub/agent"
	"github.com/feimingxliu/ub/internal/pkg/core/execution"
	"github.com/spf13/cobra"
)

func newPromptCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "Inspect prompt construction",
	}
	cmd.AddCommand(newPromptInspectCmd())
	return cmd
}

func newPromptInspectCmd() *cobra.Command {
	var jsonOutput bool
	var showContent bool
	var model string
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Inspect the local provider prompt prefix",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPromptInspect(cmd, jsonOutput, showContent, model)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON output")
	cmd.Flags().BoolVar(&showContent, "show-content", false, "include prompt section content")
	cmd.Flags().StringVar(&model, "model", "", "model id used only for token estimation")
	return cmd
}

func runPromptInspect(cmd *cobra.Command, jsonOutput, showContent bool, model string) error {
	cfg, _, err := loadConfigForCommand(cmd)
	if err != nil {
		return err
	}
	workspace, err := currentWorkspace()
	if err != nil {
		return err
	}
	mode, err := execution.ParseMode(cfg.ExecutionMode)
	if err != nil {
		return err
	}
	if strings.TrimSpace(model) == "" {
		model = cfg.DefaultModel
	}
	manifest := agent.InspectPrompt(agent.PromptInspectOptions{
		Runtime:        agentRuntimeContext(workspace),
		WorkspaceRoot:  workspace,
		Prompt:         cfg.Prompt,
		Mode:           mode,
		MemoryMaxChars: cfg.Memory.MaxChars,
		Model:          model,
		ShowContent:    showContent,
	})
	if jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(manifest)
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), renderPromptManifestText(manifest, showContent))
	return err
}

func renderPromptManifestText(manifest agent.PromptManifest, showContent bool) string {
	var out strings.Builder
	fmt.Fprintf(&out, "prompt:\n")
	fmt.Fprintf(&out, "  variant\t%s\n", manifest.Variant)
	model := manifest.Model
	if strings.TrimSpace(model) == "" {
		model = "(unspecified)"
	}
	fmt.Fprintf(&out, "  model\t%s\n", model)
	fmt.Fprintf(&out, "  total\t%d chars\t%d estimated_tokens\n", manifest.TotalChars, manifest.EstimatedTokens)
	fmt.Fprintf(&out, "sections:\n")
	for _, section := range manifest.Sections {
		fmt.Fprintf(
			&out,
			"  %d\t%s\t%s\t%s\tsource=%s\tchars=%d\ttokens=%d\ttruncated=%t\n",
			section.Position,
			section.ID,
			section.Status,
			section.Stability,
			section.Source,
			section.Chars,
			section.EstimatedTokens,
			section.Truncated,
		)
		if showContent && section.Content != "" {
			fmt.Fprintln(&out, "    content:")
			for _, line := range strings.Split(section.Content, "\n") {
				fmt.Fprintf(&out, "      | %s\n", line)
			}
		}
	}
	return out.String()
}
