package config

import (
	"testing"
	"time"

	"github.com/feimingxliu/ub/internal/reasoning"
)

func TestMergeScalarsMapsAndSlices(t *testing.T) {
	base := &Config{
		DefaultModel:    "openai/gpt-4o",
		DefaultProvider: "openai",
		Providers: map[string]ProviderConfig{
			"openai": {
				Type:    "openai",
				APIKey:  "A",
				BaseURL: "B",
			},
		},
		MCPServers: map[string]MCPServerConfig{
			"filesystem": {
				Type: "stdio",
				Args: []string{"old"},
			},
		},
	}
	override := &Config{
		DefaultModel:    "anthropic/claude-sonnet-4-7",
		DefaultProvider: "anthropic",
		Providers: map[string]ProviderConfig{
			"openai": {
				BaseURL: "C",
			},
			"anthropic": {
				Type: "anthropic",
			},
		},
		MCPServers: map[string]MCPServerConfig{
			"filesystem": {
				Type: "stdio",
				Args: []string{"new"},
			},
		},
	}

	got := Merge(base, override)
	if got.DefaultModel != "anthropic/claude-sonnet-4-7" {
		t.Fatalf("DefaultModel = %q", got.DefaultModel)
	}
	if got.DefaultProvider != "anthropic" {
		t.Fatalf("DefaultProvider = %q", got.DefaultProvider)
	}
	if got.Providers["openai"].APIKey != "" {
		t.Fatalf("provider value should be replaced wholesale, got api_key %q", got.Providers["openai"].APIKey)
	}
	if got.Providers["openai"].BaseURL != "C" {
		t.Fatalf("provider base_url = %q", got.Providers["openai"].BaseURL)
	}
	if got.Providers["anthropic"].Type != "anthropic" {
		t.Fatalf("new provider missing: %#v", got.Providers)
	}
	if got.MCPServers["filesystem"].Args[0] != "new" {
		t.Fatalf("slice field inside map value was not replaced: %#v", got.MCPServers["filesystem"].Args)
	}
}

func TestMergeDefaults(t *testing.T) {
	cleanupEnabled := false
	promptWorkspaceEnabled := false
	memoryAutoEnabled := false
	memoryAutoDisableExternal := false
	got := Merge(Defaults(), &Config{
		TUI:           TUIConfig{Theme: "light"},
		ExecutionMode: ModePlan,
		Reasoning:     reasoning.Config{Effort: reasoning.EffortHigh},
		Prompt: PromptConfig{
			WorkspaceInstructions: PromptSectionConfig{
				Enabled:  &promptWorkspaceEnabled,
				MaxChars: 2048,
			},
			GitSnapshot:  PromptSectionConfig{MaxChars: 1024},
			CompactStyle: CompactStyleShort,
		},
		ApprovalAgent: ApprovalAgentConfig{
			Provider:  "openai",
			Model:     "gpt-test",
			Reasoning: reasoning.Config{Effort: reasoning.EffortLow},
		},
		ToolsDisabled: []string{"bash"},
		Context: ContextConfig{
			TriggerRatio:    0.9,
			KeepRecentTurns: 5,
		},
		Memory: MemoryConfig{
			MaxChars: 9000,
			Auto: MemoryAutoConfig{
				Enabled:                  &memoryAutoEnabled,
				Trigger:                  "immediate",
				MaxCandidates:            2,
				MaxPromptChars:           4096,
				MinTurnsSinceExtraction:  4,
				MinNewMessages:           8,
				MinInterval:              15 * time.Minute,
				DrainTimeout:             5 * time.Second,
				DisableOnExternalContext: &memoryAutoDisableExternal,
			},
		},
		Tools: ToolsConfig{
			Job: JobToolConfig{
				MaxConcurrent:   10,
				Retention:       4 * time.Hour,
				CleanupInterval: time.Minute,
			},
		},
		Cleanup: CleanupConfig{
			Enabled:  &cleanupEnabled,
			Interval: 12 * time.Hour,
			Sessions: CleanupSessionsConfig{
				MaxAge:                7 * 24 * time.Hour,
				MinRecentPerWorkspace: 3,
			},
			Logs: CleanupLogsConfig{
				MaxSizeMB:  1,
				MaxBackups: 2,
			},
		},
		Permissions: PermissionConfig{AutoAllowWrite: true},
	})

	if got.TUI.Theme != "light" {
		t.Fatalf("theme = %q", got.TUI.Theme)
	}
	if got.ExecutionMode != ModePlan {
		t.Fatalf("execution mode = %q", got.ExecutionMode)
	}
	if got.ApprovalAgent.Provider != "openai" || got.ApprovalAgent.Model != "gpt-test" {
		t.Fatalf("approval agent = %#v", got.ApprovalAgent)
	}
	if got.Reasoning.Effort != reasoning.EffortHigh || got.ApprovalAgent.Reasoning.Effort != reasoning.EffortLow {
		t.Fatalf("reasoning = %#v approval=%#v", got.Reasoning, got.ApprovalAgent.Reasoning)
	}
	if got.Prompt.WorkspaceInstructions.Enabled == nil || *got.Prompt.WorkspaceInstructions.Enabled {
		t.Fatalf("prompt workspace enabled = %#v, want false", got.Prompt.WorkspaceInstructions.Enabled)
	}
	if got.Prompt.WorkspaceInstructions.MaxChars != 2048 ||
		got.Prompt.GitSnapshot.MaxChars != 1024 ||
		got.Prompt.CompactStyle != CompactStyleShort {
		t.Fatalf("prompt = %#v", got.Prompt)
	}
	if len(got.ToolsDisabled) != 1 || got.ToolsDisabled[0] != "bash" {
		t.Fatalf("tools disabled = %#v", got.ToolsDisabled)
	}
	if got.Context.TriggerRatio != 0.9 || got.Context.KeepRecentTurns != 5 {
		t.Fatalf("context = %#v", got.Context)
	}
	if got.Memory.MaxChars != 9000 ||
		got.Memory.Auto.Enabled == nil ||
		*got.Memory.Auto.Enabled ||
		got.Memory.Auto.MaxCandidates != 2 ||
		got.Memory.Auto.MaxPromptChars != 4096 ||
		got.Memory.Auto.Trigger != "immediate" ||
		got.Memory.Auto.MinTurnsSinceExtraction != 4 ||
		got.Memory.Auto.MinNewMessages != 8 ||
		got.Memory.Auto.MinInterval != 15*time.Minute ||
		got.Memory.Auto.DrainTimeout != 5*time.Second ||
		got.Memory.Auto.DisableOnExternalContext == nil ||
		*got.Memory.Auto.DisableOnExternalContext {
		t.Fatalf("memory = %#v", got.Memory)
	}
	if got.Tools.Job.MaxConcurrent != 10 ||
		got.Tools.Job.Retention != 4*time.Hour ||
		got.Tools.Job.CleanupInterval != time.Minute {
		t.Fatalf("tools.job = %#v", got.Tools.Job)
	}
	if got.Cleanup.CleanupEnabled() {
		t.Fatalf("cleanup enabled = true, want false")
	}
	if got.Cleanup.Interval != 12*time.Hour ||
		got.Cleanup.Sessions.MaxAge != 7*24*time.Hour ||
		got.Cleanup.Sessions.MinRecentPerWorkspace != 3 ||
		got.Cleanup.Logs.MaxSizeMB != 1 ||
		got.Cleanup.Logs.MaxBackups != 2 {
		t.Fatalf("cleanup = %#v", got.Cleanup)
	}
	if !got.Permissions.AutoAllowSafe || !got.Permissions.AutoAllowWrite {
		t.Fatalf("permissions = %#v", got.Permissions)
	}
}
