package permission

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
)

// DefaultRulesPath returns the user-level global permissions path.
func DefaultRulesPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "ub", "permissions.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "ub", "permissions.yaml"), nil
}

// LoadGlobalRules loads persisted global allow-rules. Missing or empty
// files are treated as no rules.
func LoadGlobalRules(path string) ([]Rule, error) {
	if path == "" {
		return nil, errors.New("permission rules path is empty")
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read permission rules: %w", err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var file ruleFile
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse permission rules: %w", err)
	}
	return append([]Rule(nil), file.Global...), nil
}

// SaveGlobalRule appends one global rule and atomically writes the file.
func SaveGlobalRule(path string, rule Rule) error {
	rules, err := LoadGlobalRules(path)
	if err != nil {
		return err
	}
	rules = append(rules, rule)
	return writeRuleFileAtomic(path, ruleFile{Global: rules}, nil)
}

func writeRuleFileAtomic(path string, file ruleFile, beforeRename func(tmp string) error) error {
	if path == "" {
		return errors.New("permission rules path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create permission rules directory: %w", err)
	}
	raw, err := yaml.Marshal(file)
	if err != nil {
		return fmt.Errorf("marshal permission rules: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".permissions-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp permission rules: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp permission rules: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp permission rules: %w", err)
	}
	if beforeRename != nil {
		if err := beforeRename(tmpName); err != nil {
			return err
		}
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename permission rules: %w", err)
	}
	cleanup = false
	return nil
}
