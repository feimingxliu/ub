// Package config provides layered YAML configuration loading for ub.
//
// Layering order (low to high precedence):
//
//  1. Defaults() - hard-coded sensible values
//  2. ~/.config/ub/config.yaml - per-user global
//  3. <cwd-or-ancestor>/.ub/config.yaml - per-project local
//  4. Environment variable substitution via ${VAR} / ${VAR:-default} in
//     the YAML byte stream before parsing
//  5. Optional profile overlay selected by CLI flags or UB_PROFILE
//  6. Optional CLI execution_mode override
//
// The package does NOT support hot reload, config write-back, or JSON config
// files - see docs/roadmap.md for what's scheduled where.
package config

import (
	"time"

	"github.com/feimingxliu/ub/internal/pkg/core/reasoning"
)

// Config is the merged effective configuration.
//
// Each leaf field carries both a `yaml:` and `json:` tag so that
// goccy/go-yaml (used at load time) and invopop/jsonschema (used to
// generate api/config.schema.json) see the same shape.
//
// The `Unknown` inline field swallows unknown top-level keys so YAML
// containing forward-compatible config doesn't fail to parse.
type Config struct {
	DefaultModel    string                     `yaml:"default_model,omitempty" json:"default_model,omitempty"`
	DefaultProvider string                     `yaml:"default_provider,omitempty" json:"default_provider,omitempty"`
	SmallModel      string                     `yaml:"small_model,omitempty"   json:"small_model,omitempty"`
	ExecutionMode   string                     `yaml:"execution_mode,omitempty" json:"execution_mode,omitempty"`
	MaxTurns        int                        `yaml:"max_turns,omitempty"     json:"max_turns,omitempty"`
	Reasoning       reasoning.Config           `yaml:"reasoning,omitempty"     json:"reasoning,omitempty"`
	Prompt          PromptConfig               `yaml:"prompt,omitempty"        json:"prompt,omitempty"`
	ApprovalAgent   ApprovalAgentConfig        `yaml:"approval_agent,omitempty" json:"approval_agent,omitempty"`
	Providers       map[string]ProviderConfig  `yaml:"providers,omitempty"     json:"providers,omitempty"`
	Profiles        map[string]ProfileConfig   `yaml:"profiles,omitempty"      json:"profiles,omitempty"`
	ToolsDisabled   []string                   `yaml:"tools_disabled,omitempty" json:"tools_disabled,omitempty"`
	TUI             TUIConfig                  `yaml:"tui,omitempty"           json:"tui,omitempty"`
	Permissions     PermissionConfig           `yaml:"permissions,omitempty"   json:"permissions,omitempty"`
	MCPServers      map[string]MCPServerConfig `yaml:"mcp_servers,omitempty"  json:"mcp_servers,omitempty"`
	LSPServers      map[string]LSPServerConfig `yaml:"lsp_servers,omitempty"  json:"lsp_servers,omitempty"`
	Tools           ToolsConfig                `yaml:"tools,omitempty"        json:"tools,omitempty"`
	Context         ContextConfig              `yaml:"context,omitempty"       json:"context,omitempty"`
	Cleanup         CleanupConfig              `yaml:"cleanup,omitempty"       json:"cleanup,omitempty"`
	Hooks           HooksConfig                `yaml:"hooks,omitempty"         json:"hooks,omitempty"`
	Memory          MemoryConfig               `yaml:"memory,omitempty"        json:"memory,omitempty"`

	Unknown map[string]any `yaml:",inline" json:"-"`
}

// ProfileConfig describes a runtime profile overlay. It intentionally mirrors
// the top-level runtime fields without nesting profiles recursively.
type ProfileConfig struct {
	DefaultModel    string                     `yaml:"default_model,omitempty" json:"default_model,omitempty"`
	DefaultProvider string                     `yaml:"default_provider,omitempty" json:"default_provider,omitempty"`
	SmallModel      string                     `yaml:"small_model,omitempty"   json:"small_model,omitempty"`
	ExecutionMode   string                     `yaml:"execution_mode,omitempty" json:"execution_mode,omitempty"`
	MaxTurns        int                        `yaml:"max_turns,omitempty"     json:"max_turns,omitempty"`
	Reasoning       reasoning.Config           `yaml:"reasoning,omitempty"     json:"reasoning,omitempty"`
	Prompt          PromptConfig               `yaml:"prompt,omitempty"        json:"prompt,omitempty"`
	ApprovalAgent   ApprovalAgentConfig        `yaml:"approval_agent,omitempty" json:"approval_agent,omitempty"`
	Providers       map[string]ProviderConfig  `yaml:"providers,omitempty"     json:"providers,omitempty"`
	ToolsDisabled   []string                   `yaml:"tools_disabled,omitempty" json:"tools_disabled,omitempty"`
	TUI             TUIConfig                  `yaml:"tui,omitempty"           json:"tui,omitempty"`
	Permissions     PermissionConfig           `yaml:"permissions,omitempty"   json:"permissions,omitempty"`
	MCPServers      map[string]MCPServerConfig `yaml:"mcp_servers,omitempty"  json:"mcp_servers,omitempty"`
	LSPServers      map[string]LSPServerConfig `yaml:"lsp_servers,omitempty"  json:"lsp_servers,omitempty"`
	Tools           ToolsConfig                `yaml:"tools,omitempty"        json:"tools,omitempty"`
	Context         ContextConfig              `yaml:"context,omitempty"       json:"context,omitempty"`
	Cleanup         CleanupConfig              `yaml:"cleanup,omitempty"       json:"cleanup,omitempty"`
	Memory          MemoryConfig               `yaml:"memory,omitempty"        json:"memory,omitempty"`
}

// ApprovalAgentConfig selects the secondary model used by auto mode.
type ApprovalAgentConfig struct {
	Provider  string           `yaml:"provider,omitempty"  json:"provider,omitempty"`
	Model     string           `yaml:"model,omitempty"     json:"model,omitempty"`
	Reasoning reasoning.Config `yaml:"reasoning,omitempty" json:"reasoning,omitempty"`
}

// ProviderConfig describes one LLM provider entry. The set of fields is
// intentionally narrow: only what the provider factory in I-07/I-08 will
// consume. APIKey carries `secret:"true"` so config.Redact masks it.
type ProviderConfig struct {
	Type    string                 `yaml:"type,omitempty"     json:"type,omitempty"`
	APIKey  string                 `yaml:"api_key,omitempty"  json:"api_key,omitempty"  secret:"true"`
	BaseURL string                 `yaml:"base_url,omitempty" json:"base_url,omitempty"`
	Headers map[string]string      `yaml:"headers,omitempty"  json:"headers,omitempty"`
	Timeout time.Duration          `yaml:"timeout,omitempty"  json:"timeout,omitempty"`
	Models  map[string]ModelConfig `yaml:"models,omitempty"   json:"models,omitempty"`
	Script  []ProviderScriptEvent  `yaml:"script,omitempty"   json:"script,omitempty"`
}

// ModelConfig overrides built-in model capability metadata.
type ModelConfig struct {
	SupportsReasoning bool               `yaml:"supports_reasoning,omitempty" json:"supports_reasoning,omitempty"`
	SupportedEfforts  []reasoning.Effort `yaml:"supported_efforts,omitempty"  json:"supported_efforts,omitempty"`
	DefaultEffort     reasoning.Effort   `yaml:"default_effort,omitempty"     json:"default_effort,omitempty"`
	MaxContextTokens  int                `yaml:"max_context_tokens,omitempty" json:"max_context_tokens,omitempty"`
}

const (
	CompactStyleShort      = "short"
	CompactStyleStructured = "structured"
)

// PromptConfig controls non-conversation context injected into coding-agent
// provider requests. These sections are not persisted in rollout history.
type PromptConfig struct {
	WorkspaceInstructions PromptSectionConfig `yaml:"workspace_instructions,omitempty" json:"workspace_instructions,omitempty"`
	GitSnapshot           PromptSectionConfig `yaml:"git_snapshot,omitempty"           json:"git_snapshot,omitempty"`
	CompactStyle          string              `yaml:"compact_style,omitempty"          json:"compact_style,omitempty" jsonschema:"enum=short,enum=structured"`
}

// PromptSectionConfig controls one dynamic prompt section. Enabled is a
// pointer so later layers can explicitly turn on-by-default sections off.
type PromptSectionConfig struct {
	Enabled  *bool `yaml:"enabled,omitempty"   json:"enabled,omitempty"`
	MaxChars int   `yaml:"max_chars,omitempty" json:"max_chars,omitempty"`
}

// ProviderScriptEvent is used by the fake provider to produce deterministic
// stream events from configuration.
type ProviderScriptEvent struct {
	Type             string `yaml:"type,omitempty"               json:"type,omitempty"`
	Text             string `yaml:"text,omitempty"               json:"text,omitempty"`
	Reasoning        string `yaml:"reasoning,omitempty"          json:"reasoning,omitempty"`
	ToolUseID        string `yaml:"tool_use_id,omitempty"        json:"tool_use_id,omitempty"`
	ToolName         string `yaml:"tool_name,omitempty"          json:"tool_name,omitempty"`
	Input            any    `yaml:"input,omitempty"              json:"input,omitempty"`
	InputTokens      int    `yaml:"input_tokens,omitempty"       json:"input_tokens,omitempty"`
	OutputTokens     int    `yaml:"output_tokens,omitempty"      json:"output_tokens,omitempty"`
	ReasoningTokens  int    `yaml:"reasoning_tokens,omitempty"   json:"reasoning_tokens,omitempty"`
	CacheReadTokens  int    `yaml:"cache_read_tokens,omitempty"  json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int    `yaml:"cache_write_tokens,omitempty" json:"cache_write_tokens,omitempty"`
	Error            string `yaml:"error,omitempty"              json:"error,omitempty"`
}

// TUIConfig controls how the Bubble Tea interface renders.
type TUIConfig struct {
	Theme   string `yaml:"theme,omitempty"   json:"theme,omitempty"`
	Compact bool   `yaml:"compact,omitempty" json:"compact,omitempty"`
}

// PermissionConfig controls tool approval defaults.
type PermissionConfig struct {
	AutoAllowSafe  bool `yaml:"auto_allow_safe,omitempty"  json:"auto_allow_safe,omitempty"`
	AutoAllowWrite bool `yaml:"auto_allow_write,omitempty" json:"auto_allow_write,omitempty"`
	AutoAllowExec  bool `yaml:"auto_allow_exec,omitempty"  json:"auto_allow_exec,omitempty"`
}

// MCPServerConfig configures one MCP server connection (used by I-29).
type MCPServerConfig struct {
	Type    string            `yaml:"type,omitempty"    json:"type,omitempty"` // stdio / http / sse
	Command string            `yaml:"command,omitempty" json:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty"    json:"args,omitempty"`
	URL     string            `yaml:"url,omitempty"     json:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"     json:"env,omitempty"`
}

// LSPServerConfig configures one LSP server (used by I-31).
type LSPServerConfig struct {
	Command   string   `yaml:"command,omitempty"    json:"command,omitempty"`
	Args      []string `yaml:"args,omitempty"       json:"args,omitempty"`
	FileTypes []string `yaml:"file_types,omitempty" json:"file_types,omitempty"`
}

// ToolsConfig controls built-in tool behavior.
type ToolsConfig struct {
	Job JobToolConfig `yaml:"job,omitempty" json:"job,omitempty"`
}

// JobToolConfig controls background job lifecycle limits.
type JobToolConfig struct {
	MaxConcurrent   int           `yaml:"max_concurrent,omitempty"  json:"max_concurrent,omitempty"`
	Retention       time.Duration `yaml:"retention,omitempty"       json:"retention,omitempty"`
	CleanupInterval time.Duration `yaml:"cleanup_interval,omitempty" json:"cleanup_interval,omitempty"`
}

// ContextConfig controls auto-summarization (used by I-28).
type ContextConfig struct {
	TriggerRatio        float64                 `yaml:"trigger_ratio,omitempty"         json:"trigger_ratio,omitempty"`
	KeepRecentTurns     int                     `yaml:"keep_recent_turns,omitempty"     json:"keep_recent_turns,omitempty"`
	ReserveOutputTokens int                     `yaml:"reserve_output_tokens,omitempty" json:"reserve_output_tokens,omitempty"`
	ToolResults         ContextToolResultConfig `yaml:"tool_results,omitempty"          json:"tool_results,omitempty"`
}

// ContextToolResultConfig controls model-visible tool result limiting.
type ContextToolResultConfig struct {
	InlineMaxBytes   int           `yaml:"inline_max_bytes,omitempty"  json:"inline_max_bytes,omitempty"`
	InlineMaxLines   int           `yaml:"inline_max_lines,omitempty"  json:"inline_max_lines,omitempty"`
	SpilloverEnabled *bool         `yaml:"spillover_enabled,omitempty" json:"spillover_enabled,omitempty"`
	SpilloverMaxAge  time.Duration `yaml:"spillover_max_age,omitempty" json:"spillover_max_age,omitempty"`
	FullMaxBytes     int           `yaml:"full_max_bytes,omitempty"    json:"full_max_bytes,omitempty"`
	SpilloverDir     string        `yaml:"spillover_dir,omitempty"     json:"spillover_dir,omitempty"`
}

// MemoryConfig controls how much of the persisted memory files agent
// runtime injects into the system prompt.
type MemoryConfig struct {
	MaxChars int              `yaml:"max_chars,omitempty" json:"max_chars,omitempty"`
	Auto     MemoryAutoConfig `yaml:"auto,omitempty"      json:"auto,omitempty"`
}

// MemoryAutoConfig controls LLM-assisted post-turn memory extraction.
type MemoryAutoConfig struct {
	Enabled                  *bool         `yaml:"enabled,omitempty"                     json:"enabled,omitempty"`
	Trigger                  string        `yaml:"trigger,omitempty"                     json:"trigger,omitempty" jsonschema:"enum=background,enum=immediate"`
	MaxCandidates            int           `yaml:"max_candidates,omitempty"              json:"max_candidates,omitempty"`
	MaxPromptChars           int           `yaml:"max_prompt_chars,omitempty"            json:"max_prompt_chars,omitempty"`
	MinTurnsSinceExtraction  int           `yaml:"min_turns_since_extraction,omitempty" json:"min_turns_since_extraction,omitempty"`
	MinNewMessages           int           `yaml:"min_new_messages,omitempty"            json:"min_new_messages,omitempty"`
	MinInterval              time.Duration `yaml:"min_interval,omitempty"                json:"min_interval,omitempty"`
	DrainTimeout             time.Duration `yaml:"drain_timeout,omitempty"               json:"drain_timeout,omitempty"`
	DisableOnExternalContext *bool         `yaml:"disable_on_external_context,omitempty" json:"disable_on_external_context,omitempty"`
}

// HooksConfig holds shell hook lists keyed by trigger kind: pre_tool_call,
// post_tool_call, pre_user_turn, post_user_turn. Unknown trigger kinds are
// preserved during YAML load but ignored at runtime, so forward-compat
// configurations don't fail to parse.
type HooksConfig struct {
	PreToolCall  []HookSpec `yaml:"pre_tool_call,omitempty"   json:"pre_tool_call,omitempty"`
	PostToolCall []HookSpec `yaml:"post_tool_call,omitempty"  json:"post_tool_call,omitempty"`
	PreUserTurn  []HookSpec `yaml:"pre_user_turn,omitempty"   json:"pre_user_turn,omitempty"`
	PostUserTurn []HookSpec `yaml:"post_user_turn,omitempty"  json:"post_user_turn,omitempty"`
}

// HookSpec describes one shell hook entry.
type HookSpec struct {
	Command   []string      `yaml:"command,omitempty"     json:"command,omitempty"`
	Tools     []string      `yaml:"tools,omitempty"       json:"tools,omitempty"`
	Timeout   time.Duration `yaml:"timeout,omitempty"     json:"timeout,omitempty"`
	OnFailure string        `yaml:"on_failure,omitempty"  json:"on_failure,omitempty"`
	Env       []string      `yaml:"env,omitempty"         json:"env,omitempty"`
}

// CleanupConfig controls best-effort startup cleanup for persisted sessions and
// runtime logs. Enabled is a pointer so a later layer can explicitly override
// the default true value with false.
type CleanupConfig struct {
	Enabled  *bool                 `yaml:"enabled,omitempty"  json:"enabled,omitempty"`
	Interval time.Duration         `yaml:"interval,omitempty" json:"interval,omitempty"`
	Sessions CleanupSessionsConfig `yaml:"sessions,omitempty" json:"sessions,omitempty"`
	Logs     CleanupLogsConfig     `yaml:"logs,omitempty"     json:"logs,omitempty"`
}

// CleanupSessionsConfig controls session-store pruning.
type CleanupSessionsConfig struct {
	MaxAge                time.Duration `yaml:"max_age,omitempty"                  json:"max_age,omitempty"`
	MinRecentPerWorkspace int           `yaml:"min_recent_per_workspace,omitempty" json:"min_recent_per_workspace,omitempty"`
}

// CleanupLogsConfig controls log rotation.
type CleanupLogsConfig struct {
	MaxSizeMB  int `yaml:"max_size_mb,omitempty" json:"max_size_mb,omitempty"`
	MaxBackups int `yaml:"max_backups,omitempty" json:"max_backups,omitempty"`
}

// CleanupEnabled reports whether startup cleanup is enabled after config merge.
func (c CleanupConfig) CleanupEnabled() bool {
	if c.Enabled == nil {
		return false
	}
	return *c.Enabled
}
