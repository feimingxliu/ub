package config

import "time"

const (
	DefaultPromptWorkspaceInstructionsMaxChars = 12000
	DefaultPromptGitSnapshotMaxChars           = 4000
	DefaultMemoryMaxChars                      = 4000
	DefaultMemoryAutoMaxCandidates             = 3
	DefaultMemoryAutoMaxPromptChars            = 12000
	DefaultMemoryAutoTrigger                   = "background"
	DefaultMemoryAutoMinTurnsSinceExtraction   = 3
	DefaultMemoryAutoMinNewMessages            = 6
	DefaultMemoryAutoMinInterval               = 10 * time.Minute
	DefaultMemoryAutoDrainTimeout              = 3 * time.Second
)

// Defaults returns the built-in configuration used as the lowest-priority
// layer in Load(). It contains values that let ub start sanely with no
// user configuration at all.
func Defaults() *Config {
	cleanupEnabled := true
	toolSpilloverEnabled := true
	promptWorkspaceInstructionsEnabled := true
	promptGitSnapshotEnabled := true
	memoryAutoEnabled := true
	memoryAutoDisableOnExternalContext := true
	return &Config{
		ExecutionMode: ModeWork,
		Prompt: PromptConfig{
			WorkspaceInstructions: PromptSectionConfig{
				Enabled:  &promptWorkspaceInstructionsEnabled,
				MaxChars: DefaultPromptWorkspaceInstructionsMaxChars,
			},
			GitSnapshot: PromptSectionConfig{
				Enabled:  &promptGitSnapshotEnabled,
				MaxChars: DefaultPromptGitSnapshotMaxChars,
			},
			CompactStyle: CompactStyleStructured,
		},
		TUI: TUIConfig{
			Theme: "dark",
		},
		Permissions: PermissionConfig{
			AutoAllowSafe:  true,
			AutoAllowWrite: false,
			AutoAllowExec:  false,
		},
		Tools: ToolsConfig{
			Job: JobToolConfig{
				MaxConcurrent:   50,
				Retention:       8 * time.Hour,
				CleanupInterval: 5 * time.Minute,
			},
		},
		Context: ContextConfig{
			TriggerRatio:        0.8,
			KeepRecentTurns:     3,
			ReserveOutputTokens: 12000,
			ToolResults: ContextToolResultConfig{
				InlineMaxBytes:   12288,
				InlineMaxLines:   400,
				SpilloverEnabled: &toolSpilloverEnabled,
				SpilloverMaxAge:  168 * time.Hour,
			},
		},
		Cleanup: CleanupConfig{
			Enabled:  &cleanupEnabled,
			Interval: 24 * time.Hour,
			Sessions: CleanupSessionsConfig{
				MaxAge:                30 * 24 * time.Hour,
				MinRecentPerWorkspace: 20,
			},
			Logs: CleanupLogsConfig{
				MaxSizeMB:  10,
				MaxBackups: 5,
			},
		},
		Memory: MemoryConfig{
			MaxChars: DefaultMemoryMaxChars,
			Auto: MemoryAutoConfig{
				Enabled:                  &memoryAutoEnabled,
				Trigger:                  DefaultMemoryAutoTrigger,
				MaxCandidates:            DefaultMemoryAutoMaxCandidates,
				MaxPromptChars:           DefaultMemoryAutoMaxPromptChars,
				MinTurnsSinceExtraction:  DefaultMemoryAutoMinTurnsSinceExtraction,
				MinNewMessages:           DefaultMemoryAutoMinNewMessages,
				MinInterval:              DefaultMemoryAutoMinInterval,
				DrainTimeout:             DefaultMemoryAutoDrainTimeout,
				DisableOnExternalContext: &memoryAutoDisableOnExternalContext,
			},
		},
	}
}
