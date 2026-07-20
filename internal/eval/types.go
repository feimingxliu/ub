// Package eval runs reproducible coding-agent behavior evaluations.
package eval

import "time"

const SchemaVersion = 1

const (
	FailureNone      = ""
	FailureTask      = "task"
	FailureAgent     = "agent"
	FailureAssertion = "assertion"
	FailureInternal  = "internal"
)

type Task struct {
	SchemaVersion int        `yaml:"schema_version" json:"schema_version"`
	Name          string     `yaml:"name" json:"name"`
	Description   string     `yaml:"description,omitempty" json:"description,omitempty"`
	Prompt        string     `yaml:"prompt" json:"prompt"`
	Followups     []string   `yaml:"followup_prompts,omitempty" json:"followup_prompts,omitempty"`
	Fixture       string     `yaml:"fixture,omitempty" json:"fixture,omitempty"`
	Timeout       string     `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Runtime       Runtime    `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Assertions    Assertions `yaml:"assertions" json:"assertions"`
}

// Runtime contains the small, explicit set of agent runtime settings an eval
// task may override. It deliberately excludes provider credentials, hooks,
// permissions, and arbitrary config maps.
type Runtime struct {
	MaxContextTokens *int           `yaml:"max_context_tokens,omitempty" json:"max_context_tokens,omitempty"`
	Context          RuntimeContext `yaml:"context,omitempty" json:"context,omitempty"`
}

type RuntimeContext struct {
	TriggerRatio    *float64 `yaml:"trigger_ratio,omitempty" json:"trigger_ratio,omitempty"`
	KeepRecentTurns *int     `yaml:"keep_recent_turns,omitempty" json:"keep_recent_turns,omitempty"`
}

type Assertions struct {
	Files    []FileAssertion    `yaml:"files,omitempty" json:"files,omitempty"`
	Commands []CommandAssertion `yaml:"commands,omitempty" json:"commands,omitempty"`
	Rollout  RolloutAssertions  `yaml:"rollout,omitempty" json:"rollout,omitempty"`
}

type FileAssertion struct {
	Path        string   `yaml:"path" json:"path"`
	Exists      *bool    `yaml:"exists,omitempty" json:"exists,omitempty"`
	Contains    []string `yaml:"contains,omitempty" json:"contains,omitempty"`
	NotContains []string `yaml:"not_contains,omitempty" json:"not_contains,omitempty"`
}

type CommandAssertion struct {
	Name           string   `yaml:"name,omitempty" json:"name,omitempty"`
	Run            []string `yaml:"run" json:"run"`
	ExitCode       int      `yaml:"exit_code,omitempty" json:"exit_code"`
	StdoutContains []string `yaml:"stdout_contains,omitempty" json:"stdout_contains,omitempty"`
	StderrContains []string `yaml:"stderr_contains,omitempty" json:"stderr_contains,omitempty"`
}

type RolloutAssertions struct {
	ToolsCalled          []string   `yaml:"tools_called,omitempty" json:"tools_called,omitempty"`
	ToolsNotCalled       []string   `yaml:"tools_not_called,omitempty" json:"tools_not_called,omitempty"`
	ToolOrder            []string   `yaml:"tool_order,omitempty" json:"tool_order,omitempty"`
	ToolOrderAny         [][]string `yaml:"tool_order_any,omitempty" json:"tool_order_any,omitempty"`
	ToolsCalledAny       [][]string `yaml:"tools_called_any,omitempty" json:"tools_called_any,omitempty"`
	AssistantContains    []string   `yaml:"assistant_contains,omitempty" json:"assistant_contains,omitempty"`
	AssistantNotContains []string   `yaml:"assistant_not_contains,omitempty" json:"assistant_not_contains,omitempty"`
	ContextActions       []string   `yaml:"context_actions,omitempty" json:"context_actions,omitempty"`
}

type TaskFile struct {
	Task Task
	Path string
	Dir  string
}

type Metrics struct {
	Duration         time.Duration     `json:"-"`
	DurationMillis   int64             `json:"duration_ms"`
	Turns            int               `json:"turns"`
	InputTokens      int               `json:"input_tokens"`
	OutputTokens     int               `json:"output_tokens"`
	ReasoningTokens  int               `json:"reasoning_tokens"`
	CacheReadTokens  int               `json:"cache_read_tokens"`
	CacheWriteTokens int               `json:"cache_write_tokens"`
	ToolCalls        []string          `json:"tool_calls"`
	ContextDecisions []ContextDecision `json:"context_decisions"`
}

type ContextDecision struct {
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

type Observation struct {
	SessionID     string
	Provider      string
	Model         string
	AssistantText string
	Metrics       Metrics
}

type AssertionResult struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}

type Report struct {
	Task            string            `json:"task"`
	Passed          bool              `json:"passed"`
	FailureCategory string            `json:"failure_category,omitempty"`
	Failure         string            `json:"failure,omitempty"`
	Provider        string            `json:"provider,omitempty"`
	Model           string            `json:"model,omitempty"`
	SessionID       string            `json:"session_id,omitempty"`
	Workspace       string            `json:"workspace,omitempty"`
	Runtime         Runtime           `json:"runtime"`
	Metrics         Metrics           `json:"metrics"`
	Assertions      []AssertionResult `json:"assertions"`
	AgentStderr     string            `json:"agent_stderr,omitempty"`
}

func (r Runtime) Empty() bool {
	return r.MaxContextTokens == nil && r.Context.TriggerRatio == nil && r.Context.KeepRecentTurns == nil
}

func (a Assertions) Empty() bool {
	r := a.Rollout
	return len(a.Files) == 0 && len(a.Commands) == 0 && len(r.ToolsCalled) == 0 &&
		len(r.ToolsNotCalled) == 0 && len(r.ToolOrder) == 0 && len(r.ToolOrderAny) == 0 && len(r.ToolsCalledAny) == 0 &&
		len(r.AssistantContains) == 0 && len(r.AssistantNotContains) == 0 && len(r.ContextActions) == 0
}
