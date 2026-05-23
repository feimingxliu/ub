package config

import "time"

// Defaults returns the built-in configuration used as the lowest-priority
// layer in Load(). It contains values that let ub start sanely with no
// user configuration at all.
func Defaults() *Config {
	cleanupEnabled := true
	toolSpilloverEnabled := true
	return &Config{
		ExecutionMode: ModeWork,
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
	}
}
