package eval

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

var taskNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func ResolveTaskPath(workspace, nameOrPath string) (string, error) {
	value := strings.TrimSpace(nameOrPath)
	if value == "" {
		return "", errors.New("eval task is required")
	}
	if taskNamePattern.MatchString(value) {
		value = filepath.Join(workspace, "docs", "eval-tasks", value+".yaml")
	} else if !filepath.IsAbs(value) {
		value = filepath.Join(workspace, value)
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve eval task path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("open eval task %q: %w", nameOrPath, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("eval task %q is not a regular file", abs)
	}
	return abs, nil
}

func LoadTask(path string) (TaskFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TaskFile{}, fmt.Errorf("read eval task: %w", err)
	}
	var task Task
	if err := yaml.Unmarshal(data, &task); err != nil {
		return TaskFile{}, fmt.Errorf("decode eval task: %w", err)
	}
	if err := validateRuntimeFields(data); err != nil {
		return TaskFile{}, err
	}
	if err := ValidateTask(task); err != nil {
		return TaskFile{}, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return TaskFile{}, fmt.Errorf("resolve eval task: %w", err)
	}
	return TaskFile{Task: task, Path: abs, Dir: filepath.Dir(abs)}, nil
}

func ValidateTask(task Task) error {
	if task.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported eval task schema_version %d (want %d)", task.SchemaVersion, SchemaVersion)
	}
	if !taskNamePattern.MatchString(strings.TrimSpace(task.Name)) {
		return fmt.Errorf("invalid eval task name %q", task.Name)
	}
	if strings.TrimSpace(task.Prompt) == "" {
		return errors.New("eval task prompt is required")
	}
	if err := validateRuntime(task.Runtime); err != nil {
		return err
	}
	for i, prompt := range task.Followups {
		if strings.TrimSpace(prompt) == "" {
			return fmt.Errorf("eval task followup_prompts[%d] is empty", i)
		}
	}
	if task.Assertions.Empty() {
		return errors.New("eval task must declare at least one assertion")
	}
	if task.Timeout != "" {
		if duration, err := time.ParseDuration(task.Timeout); err != nil || duration <= 0 {
			return fmt.Errorf("invalid eval task timeout %q", task.Timeout)
		}
	}
	if task.Fixture != "" {
		if err := validateRelativePath(task.Fixture); err != nil {
			return fmt.Errorf("invalid fixture: %w", err)
		}
	}
	for i, assertion := range task.Assertions.Files {
		if err := validateRelativePath(assertion.Path); err != nil {
			return fmt.Errorf("invalid file assertion %d: %w", i+1, err)
		}
		if assertion.Exists == nil && len(assertion.Contains) == 0 && len(assertion.NotContains) == 0 {
			return fmt.Errorf("file assertion %d has no checks", i+1)
		}
	}
	for i, assertion := range task.Assertions.Commands {
		if len(assertion.Run) == 0 || strings.TrimSpace(assertion.Run[0]) == "" {
			return fmt.Errorf("command assertion %d run is required", i+1)
		}
	}
	for i, group := range task.Assertions.Rollout.ToolsCalledAny {
		if len(group) == 0 {
			return fmt.Errorf("rollout tools_called_any group %d is empty", i+1)
		}
	}
	for i, order := range task.Assertions.Rollout.ToolOrderAny {
		if len(order) == 0 {
			return fmt.Errorf("rollout tool_order_any sequence %d is empty", i+1)
		}
	}
	return nil
}

func validateRuntime(runtime Runtime) error {
	if runtime.MaxContextTokens != nil && *runtime.MaxContextTokens <= 0 {
		return errors.New("eval task runtime.max_context_tokens must be positive")
	}
	if ratio := runtime.Context.TriggerRatio; ratio != nil && (*ratio <= 0 || *ratio > 1) {
		return errors.New("eval task runtime.context.trigger_ratio must be greater than 0 and at most 1")
	}
	if runtime.Context.KeepRecentTurns != nil && *runtime.Context.KeepRecentTurns <= 0 {
		return errors.New("eval task runtime.context.keep_recent_turns must be positive")
	}
	return nil
}

func validateRuntimeFields(data []byte) error {
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("decode eval task fields: %w", err)
	}
	rawRuntime, ok := document["runtime"]
	if !ok || rawRuntime == nil {
		return nil
	}
	runtime, ok := rawRuntime.(map[string]any)
	if !ok {
		return errors.New("eval task runtime must be an object")
	}
	for key := range runtime {
		if key != "max_context_tokens" && key != "context" {
			return fmt.Errorf("eval task runtime contains unknown field %q", key)
		}
	}
	rawContext, ok := runtime["context"]
	if !ok || rawContext == nil {
		return nil
	}
	context, ok := rawContext.(map[string]any)
	if !ok {
		return errors.New("eval task runtime.context must be an object")
	}
	for key := range context {
		if key != "trigger_ratio" && key != "keep_recent_turns" {
			return fmt.Errorf("eval task runtime.context contains unknown field %q", key)
		}
	}
	return nil
}

func validateRelativePath(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("path is empty")
	}
	if filepath.IsAbs(name) {
		return fmt.Errorf("absolute path %q is not allowed", name)
	}
	clean := filepath.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q escapes its root", name)
	}
	return nil
}

func PrepareFixture(task TaskFile, workspace string) error {
	if task.Task.Fixture == "" {
		return nil
	}
	source := filepath.Join(task.Dir, filepath.Clean(task.Task.Fixture))
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("open fixture: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("fixture %q must be a directory without a symlink root", task.Task.Fixture)
	}
	destination, err := os.OpenRoot(workspace)
	if err != nil {
		return fmt.Errorf("open eval workspace: %w", err)
	}
	defer destination.Close()
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("fixture contains symlink %q", rel)
		}
		if entry.IsDir() {
			return destination.MkdirAll(rel, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("fixture contains non-regular file %q", rel)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := destination.WriteFile(rel, data, info.Mode().Perm()); err != nil {
			return fmt.Errorf("copy fixture file %q: %w", rel, err)
		}
		return nil
	})
}
