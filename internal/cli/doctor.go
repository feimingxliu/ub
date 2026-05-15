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

	"github.com/feimingxliu/ub/internal/config"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var plain bool
	var suggest bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check local ub configuration and development environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd, plain, suggest)
		},
	}
	cmd.Flags().BoolVar(&plain, "plain", false, "disable styled output")
	cmd.Flags().BoolVar(&suggest, "suggest", false, "print a suggested dev profile snippet")
	return cmd
}

func runDoctor(cmd *cobra.Command, plain, suggest bool) error {
	_ = plain
	cfg, _, err := loadConfigForCommand(cmd)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	ctx := cmd.Context()

	if _, err := fmt.Fprintln(out, "providers:"); err != nil {
		return err
	}
	for _, name := range sortedProviderNames(cfg.Providers) {
		result := checkProvider(ctx, name, cfg.Providers[name])
		if _, err := fmt.Fprintf(out, "  %s\t%s\t%s\n", result.Name, result.Type, result.Status); err != nil {
			return err
		}
		for _, model := range result.Models {
			if _, err := fmt.Fprintf(out, "    model\t%s\t%s\n", model, toolSupport(model)); err != nil {
				return err
			}
		}
		if result.Message != "" {
			if _, err := fmt.Fprintf(out, "    note\t%s\n", result.Message); err != nil {
				return err
			}
		}
	}

	if _, err := fmt.Fprintln(out, "commands:"); err != nil {
		return err
	}
	for _, name := range []string{"rg", "gopls", "typescript-language-server", "npx"} {
		status := "missing"
		if path, err := exec.LookPath(name); err == nil {
			status = "found " + path
		}
		if _, err := fmt.Fprintf(out, "  %s\t%s\n", name, status); err != nil {
			return err
		}
	}

	if suggest {
		_, err := fmt.Fprint(out, suggestedDevProfile(cfg))
		return err
	}
	return nil
}

type providerCheck struct {
	Name    string
	Type    string
	Status  string
	Models  []string
	Message string
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
		baseURL := strings.TrimSpace(cfg.BaseURL)
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		fillOpenAIModels(ctx, &result, baseURL, cfg)
	case "openai-compat":
		if strings.TrimSpace(cfg.BaseURL) == "" {
			result.Status = "NO_BASE_URL"
			return result
		}
		fillOpenAIModels(ctx, &result, cfg.BaseURL, cfg)
	case "ollama":
		baseURL := strings.TrimSpace(cfg.BaseURL)
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		fillOllamaModels(ctx, &result, baseURL, cfg)
	case "anthropic":
		if strings.TrimSpace(cfg.APIKey) == "" {
			result.Status = "NO_API_KEY"
		} else {
			result.Status = "configured"
		}
	default:
		result.Status = "unknown_provider_type"
	}
	return result
}

func fillOpenAIModels(ctx context.Context, result *providerCheck, baseURL string, cfg config.ProviderConfig) {
	req, err := newDoctorRequest(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", cfg)
	if err != nil {
		result.Status = "error"
		result.Message = err.Error()
		return
	}
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := doDoctorJSON(req, &body, cfg.Timeout); err != nil {
		result.Status = "error"
		result.Message = err.Error()
		return
	}
	result.Status = "reachable"
	for _, item := range body.Data {
		if item.ID != "" {
			result.Models = append(result.Models, item.ID)
		}
	}
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
	if timeout <= 0 || timeout > 5*time.Second {
		timeout = 2 * time.Second
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
