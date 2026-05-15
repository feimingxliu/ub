package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
)

const (
	ModeDefault      = "default"
	ModePlan         = "plan"
	ModeAgentApprove = "agent-approve"
)

// LoadOptions selects runtime overlays that are not part of config file
// discovery itself.
type LoadOptions struct {
	Profile       string
	Dev           bool
	ExecutionMode string
}

// Load reads, expands, parses, and merges all configuration layers and
// returns the effective Config along with the list of files actually
// read (in merge order; missing layers are omitted).
//
// A missing file is not an error; an unparseable file IS.
func Load() (*Config, []string, error) {
	return LoadWithOptions(LoadOptions{})
}

// LoadWithOptions behaves like Load and then applies profile and CLI mode
// overlays.
func LoadWithOptions(opts LoadOptions) (*Config, []string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, fmt.Errorf("get cwd: %w", err)
	}
	return loadFromDirsWithOptions(cwd, opts)
}

// loadFromDirs is the testable core: it takes the cwd explicitly so
// tests can simulate arbitrary working directories.
func loadFromDirs(cwd string) (*Config, []string, error) {
	return loadFromDirsWithOptions(cwd, LoadOptions{})
}

func loadFromDirsWithOptions(cwd string, opts LoadOptions) (*Config, []string, error) {
	var loaded []string

	globalPath, err := globalConfigPath()
	if err != nil {
		return nil, nil, fmt.Errorf("locate global config: %w", err)
	}

	globalCfg, ok, err := readConfigFile(globalPath)
	if err != nil {
		return nil, nil, err
	}
	if ok {
		loaded = append(loaded, globalPath)
	}

	localPath := localConfigPath(cwd)
	var localCfg *Config
	if localPath != "" {
		var found bool
		localCfg, found, err = readConfigFile(localPath)
		if err != nil {
			return nil, nil, err
		}
		if found {
			loaded = append(loaded, localPath)
		}
	}

	merged := Merge(Defaults(), globalCfg, localCfg)
	if err := applyRuntimeOptions(merged, opts); err != nil {
		return nil, nil, err
	}
	return merged, loaded, nil
}

// readConfigFile reads, env-expands, and YAML-decodes one file. The
// "found" return is true only when the file exists and was successfully
// parsed; nil + false + nil signals "treat as missing layer".
func readConfigFile(path string) (*Config, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}

	expanded := Expand(raw)

	var cfg Config
	if err := yaml.Unmarshal(expanded, &cfg); err != nil {
		// goccy/go-yaml errors already carry line/column info in their
		// String form; prepend the path so the user can locate it.
		return nil, false, fmt.Errorf("parse %s: %w", path, err)
	}
	return &cfg, true, nil
}

func applyRuntimeOptions(cfg *Config, opts LoadOptions) error {
	profileName, err := selectedProfile(opts)
	if err != nil {
		return err
	}
	if profileName != "" {
		if err := cfg.ApplyProfile(profileName); err != nil {
			return err
		}
	}
	mode := strings.TrimSpace(opts.ExecutionMode)
	if mode != "" {
		if err := ValidateExecutionMode(mode); err != nil {
			return err
		}
		cfg.ExecutionMode = mode
	}
	return ValidateExecutionMode(cfg.ExecutionMode)
}

func selectedProfile(opts LoadOptions) (string, error) {
	profile := strings.TrimSpace(opts.Profile)
	if opts.Dev {
		if profile != "" {
			return "", errors.New("cannot use --dev with --profile")
		}
		return "dev", nil
	}
	if profile != "" {
		return profile, nil
	}
	return strings.TrimSpace(os.Getenv("UB_PROFILE")), nil
}

// ApplyProfile overlays one named profile onto cfg in place.
func (c *Config) ApplyProfile(name string) error {
	if c == nil {
		return errors.New("config is nil")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	profile, ok := c.Profiles[name]
	if !ok {
		return fmt.Errorf("profile %q not found", name)
	}
	if err := ValidateExecutionMode(profile.ExecutionMode); err != nil {
		return fmt.Errorf("profile %q: %w", name, err)
	}
	overlay := profile.toConfig()
	merged := Merge(c, overlay)
	*c = *merged
	return nil
}

func (p ProfileConfig) toConfig() *Config {
	return &Config{
		DefaultModel:  p.DefaultModel,
		SmallModel:    p.SmallModel,
		ExecutionMode: p.ExecutionMode,
		ApprovalAgent: p.ApprovalAgent,
		Providers:     p.Providers,
		ToolsDisabled: p.ToolsDisabled,
		TUI:           p.TUI,
		Permissions:   p.Permissions,
		MCPServers:    p.MCPServers,
		LSPServers:    p.LSPServers,
		Context:       p.Context,
	}
}

// ValidateExecutionMode checks the known execution mode strings.
func ValidateExecutionMode(mode string) error {
	switch strings.TrimSpace(mode) {
	case "", ModeDefault, ModePlan, ModeAgentApprove:
		return nil
	default:
		return fmt.Errorf("unknown execution_mode %q", mode)
	}
}
