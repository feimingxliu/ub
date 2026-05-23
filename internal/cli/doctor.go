package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"time"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/provider/anthropic"
	"github.com/feimingxliu/ub/internal/provider/openai"
	mcptool "github.com/feimingxliu/ub/internal/tool/mcp"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var plain bool
	var suggest bool
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check local ub configuration and development environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd, plain, suggest, jsonOutput)
		},
	}
	cmd.Flags().BoolVar(&plain, "plain", false, "disable styled output")
	cmd.Flags().BoolVar(&suggest, "suggest", false, "print a suggested dev profile snippet")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "print machine-readable JSON output")
	return cmd
}

func runDoctor(cmd *cobra.Command, plain, suggest, jsonOutput bool) error {
	cfg, _, err := loadConfigForCommand(cmd)
	if err != nil {
		return err
	}
	if jsonOutput {
		report := collectDoctorReport(cmd.Context(), cfg, suggest)
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	report, err := renderDoctorText(cmd.Context(), cfg, plain, suggest)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), report)
	return err
}

func renderDoctorText(ctx context.Context, cfg *config.Config, plain, suggest bool) (string, error) {
	report := collectDoctorReport(ctx, cfg, suggest)
	var out strings.Builder
	style := doctorStyle{plain: plain}

	if _, err := fmt.Fprintln(&out, style.header("providers:")); err != nil {
		return "", err
	}
	for _, result := range report.Providers {
		if _, err := fmt.Fprintf(&out, "  %s\t%s\t%s\n", result.Name, result.Type, style.status(result.Status)); err != nil {
			return "", err
		}
		for _, model := range result.Models {
			if _, err := fmt.Fprintf(&out, "    model\t%s\t%s\n", model, toolSupport(model)); err != nil {
				return "", err
			}
		}
		if result.Message != "" {
			if _, err := fmt.Fprintf(&out, "    note\t%s\n", result.Message); err != nil {
				return "", err
			}
		}
	}

	if _, err := fmt.Fprintln(&out, style.header("mcp:")); err != nil {
		return "", err
	}
	if len(report.MCP) == 0 {
		if _, err := fmt.Fprintln(&out, "  none\t-\tnot_configured"); err != nil {
			return "", err
		}
	} else {
		for _, result := range report.MCP {
			if _, err := fmt.Fprintf(&out, "  %s\t%s\t%s\n", result.Name, result.Type, style.status(result.Status)); err != nil {
				return "", err
			}
			if result.ToolCount > 0 {
				if _, err := fmt.Fprintf(&out, "    tools\t%d\n", result.ToolCount); err != nil {
					return "", err
				}
			}
			if result.Error != "" {
				if _, err := fmt.Fprintf(&out, "    note\t%s\n", result.Error); err != nil {
					return "", err
				}
			}
		}
	}

	if _, err := fmt.Fprintln(&out, style.header("commands:")); err != nil {
		return "", err
	}
	for _, result := range report.Commands {
		if _, err := fmt.Fprintf(&out, "  %s\t%s\n", result.Name, style.status(result.Status)); err != nil {
			return "", err
		}
	}

	if report.SuggestedDevProfile != "" {
		if _, err := fmt.Fprint(&out, report.SuggestedDevProfile); err != nil {
			return "", err
		}
	}
	return out.String(), nil
}

type doctorReport struct {
	Providers           []providerCheck   `json:"providers"`
	MCP                 []doctorMCPStatus `json:"mcp"`
	Commands            []doctorCommand   `json:"commands"`
	SuggestedDevProfile string            `json:"suggested_dev_profile,omitempty"`
}

type doctorMCPStatus struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	ToolCount int    `json:"tool_count,omitempty"`
	Error     string `json:"error,omitempty"`
}

type doctorCommand struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Path   string `json:"path,omitempty"`
}

func collectDoctorReport(ctx context.Context, cfg *config.Config, suggest bool) doctorReport {
	report := doctorReport{
		Providers: make([]providerCheck, 0, len(cfg.Providers)),
		Commands:  make([]doctorCommand, 0, 4),
	}
	for _, name := range sortedProviderNames(cfg.Providers) {
		report.Providers = append(report.Providers, checkProvider(ctx, name, cfg.Providers[name]))
	}
	for _, result := range mcptool.CheckConfigured(ctx, cfg.MCPServers) {
		status := doctorMCPStatus{
			Name:      result.Name,
			Type:      result.Type,
			Status:    result.Status,
			ToolCount: result.ToolCount,
		}
		if result.Err != nil {
			status.Error = result.Err.Error()
		}
		report.MCP = append(report.MCP, status)
	}
	for _, name := range []string{"rg", "gopls", "typescript-language-server", "npx"} {
		check := doctorCommand{Name: name, Status: "missing"}
		if path, err := exec.LookPath(name); err == nil {
			check.Path = path
			check.Status = "found " + path
		}
		report.Commands = append(report.Commands, check)
	}
	if suggest {
		report.SuggestedDevProfile = suggestedDevProfile(cfg)
	}
	return report
}

type doctorStyle struct {
	plain bool
}

func (s doctorStyle) header(text string) string {
	if s.plain {
		return text
	}
	return "\x1b[1m" + text + "\x1b[0m"
}

func (s doctorStyle) status(text string) string {
	if s.plain {
		return text
	}
	lower := strings.ToLower(text)
	switch {
	case lower == "reachable", lower == "configured", lower == "connected", strings.HasPrefix(lower, "found "):
		return "\x1b[32m" + text + "\x1b[0m"
	case lower == "offline":
		return "\x1b[36m" + text + "\x1b[0m"
	case strings.HasPrefix(lower, "no_"), lower == "missing", lower == "not_configured":
		return "\x1b[33m" + text + "\x1b[0m"
	case lower == "error", lower == "unknown_provider_type":
		return "\x1b[31m" + text + "\x1b[0m"
	default:
		return text
	}
}

type providerCheck struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Status  string   `json:"status"`
	Models  []string `json:"models,omitempty"`
	Message string   `json:"message,omitempty"`
}

func checkProvider(ctx context.Context, name string, cfg config.ProviderConfig) providerCheck {
	result := providerCheck{Name: name, Type: cfg.Type}
	switch strings.TrimSpace(cfg.Type) {
	case "fake":
		result.Status = "offline"
	case "openai":
		if strings.TrimSpace(cfg.APIKey) == "" {
			result.Status = "NO_API_KEY"
			return result
		}
		fillOpenAIModels(ctx, &result, cfg)
	case "openai-compat":
		if strings.TrimSpace(cfg.BaseURL) == "" {
			result.Status = "NO_BASE_URL"
			return result
		}
		fillOpenAIModels(ctx, &result, cfg)
	case "ollama":
		baseURL := strings.TrimSpace(cfg.BaseURL)
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		fillOllamaModels(ctx, &result, baseURL, cfg)
	case "anthropic":
		if strings.TrimSpace(cfg.APIKey) == "" {
			result.Status = "NO_API_KEY"
			return result
		}
		fillAnthropicModels(ctx, &result, cfg)
	default:
		result.Status = "unknown_provider_type"
	}
	return result
}

func fillOpenAIModels(ctx context.Context, result *providerCheck, cfg config.ProviderConfig) {
	client := openai.BuildClient(cfg)
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	pager := client.Models.ListAutoPaging(ctx)
	for pager.Next() {
		if id := strings.TrimSpace(pager.Current().ID); id != "" {
			result.Models = append(result.Models, id)
		}
	}
	if err := pager.Err(); err != nil {
		result.Status = "error"
		result.Message = err.Error()
		return
	}
	result.Status = "reachable"
	sort.Strings(result.Models)
}

func fillAnthropicModels(ctx context.Context, result *providerCheck, cfg config.ProviderConfig) {
	client := anthropic.BuildClient(cfg)
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	pager := client.Models.ListAutoPaging(ctx, sdk.ModelListParams{})
	for pager.Next() {
		if id := strings.TrimSpace(pager.Current().ID); id != "" {
			result.Models = append(result.Models, id)
		}
	}
	if err := pager.Err(); err != nil {
		result.Status = "error"
		result.Message = err.Error()
		return
	}
	result.Status = "reachable"
	sort.Strings(result.Models)
}

func fillOllamaModels(ctx context.Context, result *providerCheck, baseURL string, cfg config.ProviderConfig) {
	req, err := newDoctorRequest(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/tags", cfg)
	if err != nil {
		result.Status = "error"
		result.Message = err.Error()
		return
	}
	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := doDoctorJSON(req, &body, cfg.Timeout); err != nil {
		result.Status = "error"
		result.Message = err.Error()
		return
	}
	result.Status = "reachable"
	for _, item := range body.Models {
		if item.Name != "" {
			result.Models = append(result.Models, item.Name)
		}
	}
	sort.Strings(result.Models)
}

func newDoctorRequest(ctx context.Context, method, url string, cfg config.ProviderConfig) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	for key, value := range cfg.Headers {
		req.Header.Set(key, value)
	}
	return req, nil
}

func doDoctorJSON(req *http.Request, out any, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("status %d", res.StatusCode)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func sortedProviderNames(providers map[string]config.ProviderConfig) []string {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func toolSupport(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "gpt-4"), strings.Contains(m, "gpt-5"), strings.Contains(m, "claude"), strings.Contains(m, "qwen"):
		return "tools=yes"
	default:
		return "tools=unknown"
	}
}

func suggestedDevProfile(cfg *config.Config) string {
	provider := "vllm-local"
	model := "Qwen2.5-Coder-7B-Instruct"
	for name, providerCfg := range cfg.Providers {
		if providerCfg.Type == "openai-compat" || providerCfg.Type == "ollama" {
			provider = name
			break
		}
	}
	return fmt.Sprintf(`
suggested dev profile:
profiles:
  dev:
    default_model: %s/%s
    small_model: %s/%s
    execution_mode: plan
`, provider, model, provider, model)
}
