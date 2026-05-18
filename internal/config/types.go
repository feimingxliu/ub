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

import "time"

import "github.com/feimingxliu/ub/internal/reasoning"

// Config is the merged effective configuration.
//
// Each leaf field carries both a `yaml:` and `json:` tag so that
// goccy/go-yaml (used at load time) and invopop/jsonschema (used to
// generate schema/config.schema.json) see the same shape.
//
// The `Unknown` inline field swallows unknown top-level keys so YAML
// containing forward-compatible config doesn't fail to parse.
type Config struct {
	DefaultModel    string                     `yaml:"default_model,omitempty" json:"default_model,omitempty"`
	DefaultProvider string                     `yaml:"default_provider,omitempty" json:"default_provider,omitempty"`
	SmallModel      string                     `yaml:"small_model,omitempty"   json:"small_model,omitempty"`
	ExecutionMode   string                     `yaml:"execution_mode,omitempty" json:"execution_mode,omitempty"`
	Reasoning       reasoning.Config           `yaml:"reasoning,omitempty"     json:"reasoning,omitempty"`
	ApprovalAgent   ApprovalAgentConfig        `yaml:"approval_agent,omitempty" json:"approval_agent,omitempty"`
	Providers       map[string]ProviderConfig  `yaml:"providers,omitempty"     json:"providers,omitempty"`
	Profiles        map[string]ProfileConfig   `yaml:"profiles,omitempty"      json:"profiles,omitempty"`
	ToolsDisabled   []string                   `yaml:"tools_disabled,omitempty" json:"tools_disabled,omitempty"`
	TUI             TUIConfig                  `yaml:"tui,omitempty"           json:"tui,omitempty"`
	Permissions     PermissionConfig           `yaml:"permissions,omitempty"   json:"permissions,omitempty"`
	MCPServers      map[string]MCPServerConfig `yaml:"mcp_servers,omitempty"  json:"mcp_servers,omitempty"`
	LSPServers      map[string]LSPServerConfig `yaml:"lsp_servers,omitempty"  json:"lsp_servers,omitempty"`
	Context         ContextConfig              `yaml:"context,omitempty"       json:"context,omitempty"`

	Unknown map[string]any `yaml:",inline" json:"-"`
}

// ProfileConfig describes a runtime profile overlay. It intentionally mirrors
// the top-level runtime fields without nesting profiles recursively.
type ProfileConfig struct {
	DefaultModel    string                     `yaml:"default_model,omitempty" json:"default_model,omitempty"`
	DefaultProvider string                     `yaml:"default_provider,omitempty" json:"default_provider,omitempty"`
	SmallModel      string                     `yaml:"small_model,omitempty"   json:"small_model,omitempty"`
	ExecutionMode   string                     `yaml:"execution_mode,omitempty" json:"execution_mode,omitempty"`
	Reasoning       reasoning.Config           `yaml:"reasoning,omitempty"     json:"reasoning,omitempty"`
	ApprovalAgent   ApprovalAgentConfig        `yaml:"approval_agent,omitempty" json:"approval_agent,omitempty"`
	Providers       map[string]ProviderConfig  `yaml:"providers,omitempty"     json:"providers,omitempty"`
	ToolsDisabled   []string                   `yaml:"tools_disabled,omitempty" json:"tools_disabled,omitempty"`
	TUI             TUIConfig                  `yaml:"tui,omitempty"           json:"tui,omitempty"`
	Permissions     PermissionConfig           `yaml:"permissions,omitempty"   json:"permissions,omitempty"`
	MCPServers      map[string]MCPServerConfig `yaml:"mcp_servers,omitempty"  json:"mcp_servers,omitempty"`
	LSPServers      map[string]LSPServerConfig `yaml:"lsp_servers,omitempty"  json:"lsp_servers,omitempty"`
	Context         ContextConfig              `yaml:"context,omitempty"       json:"context,omitempty"`
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

// ProviderScriptEvent is used by the fake provider to produce deterministic
// stream events from configuration.
type ProviderScriptEvent struct {
	Type         string `yaml:"type,omitempty"          json:"type,omitempty"`
	Text         string `yaml:"text,omitempty"          json:"text,omitempty"`
	Reasoning    string `yaml:"reasoning,omitempty"     json:"reasoning,omitempty"`
	ToolUseID    string `yaml:"tool_use_id,omitempty"   json:"tool_use_id,omitempty"`
	ToolName     string `yaml:"tool_name,omitempty"     json:"tool_name,omitempty"`
	Input        any    `yaml:"input,omitempty"         json:"input,omitempty"`
	InputTokens  int    `yaml:"input_tokens,omitempty"  json:"input_tokens,omitempty"`
	OutputTokens int    `yaml:"output_tokens,omitempty" json:"output_tokens,omitempty"`
	Error        string `yaml:"error,omitempty"         json:"error,omitempty"`
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

// ContextConfig controls auto-summarization (used by I-28).
type ContextConfig struct {
	TriggerRatio    float64 `yaml:"trigger_ratio,omitempty"    json:"trigger_ratio,omitempty"`
	KeepRecentTurns int     `yaml:"keep_recent_turns,omitempty" json:"keep_recent_turns,omitempty"`
}
