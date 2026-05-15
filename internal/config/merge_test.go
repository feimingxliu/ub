package config

import "testing"

func TestMergeScalarsMapsAndSlices(t *testing.T) {
	base := &Config{
		DefaultModel: "openai/gpt-4o",
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
		DefaultModel: "anthropic/claude-sonnet-4-7",
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
	got := Merge(Defaults(), &Config{
		TUI:           TUIConfig{Theme: "light"},
		ExecutionMode: ModePlan,
		ApprovalAgent: ApprovalAgentConfig{
			Provider: "openai",
			Model:    "gpt-test",
		},
		ToolsDisabled: []string{"bash"},
		Context: ContextConfig{
			TriggerRatio:    0.9,
			KeepRecentTurns: 5,
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
	if len(got.ToolsDisabled) != 1 || got.ToolsDisabled[0] != "bash" {
		t.Fatalf("tools disabled = %#v", got.ToolsDisabled)
	}
	if got.Context.TriggerRatio != 0.9 || got.Context.KeepRecentTurns != 5 {
		t.Fatalf("context = %#v", got.Context)
	}
	if !got.Permissions.AutoAllowSafe || !got.Permissions.AutoAllowWrite {
		t.Fatalf("permissions = %#v", got.Permissions)
	}
}
