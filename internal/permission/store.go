package permission

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
)

// ProjectRulesPath returns the project-local permissions path for a workspace.
func ProjectRulesPath(workspace string) (string, error) {
	workspace = filepath.Clean(workspace)
	if workspace == "." || workspace == "" {
		return "", errors.New("permission workspace is empty")
	}
	return filepath.Join(workspace, ".ub", "permissions.yaml"), nil
}

// LoadProjectRules loads persisted project-local Claude-style permission
// rules. Missing or empty files are treated as no rules.
func LoadProjectRules(path string) ([]Rule, []Rule, []Rule, error) {
	file, err := loadRuleFile(path)
	if err != nil {
		return nil, nil, nil, err
	}
	return parsePermissionRules(file.Permissions)
}

// SaveProjectRule appends one project-local rule and atomically writes the file.
func SaveProjectRule(path string, action RuleAction, rule string) error {
	return SaveProjectRules(path, action, []string{rule})
}

// SaveProjectRules appends project-local rules and atomically writes the file.
func SaveProjectRules(path string, action RuleAction, rules []string) error {
	file, err := loadRuleFile(path)
	if err != nil {
		return err
	}
	switch action {
	case RuleAllow:
		file.Permissions.Allow = append(file.Permissions.Allow, rules...)
	case RuleAsk:
		file.Permissions.Ask = append(file.Permissions.Ask, rules...)
	case RuleDeny:
		file.Permissions.Deny = append(file.Permissions.Deny, rules...)
	default:
		return fmt.Errorf("unknown permission rule action %q", action)
	}
	return writeRuleFileAtomic(path, file, nil)
}

func loadRuleFile(path string) (ruleFile, error) {
	if path == "" {
		return ruleFile{}, errors.New("permission rules path is empty")
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ruleFile{}, nil
	}
	if err != nil {
		return ruleFile{}, fmt.Errorf("read permission rules: %w", err)
	}
	if len(raw) == 0 {
		return ruleFile{}, nil
	}
	var file ruleFile
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return ruleFile{}, fmt.Errorf("parse permission rules: %w", err)
	}
	return file, nil
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
