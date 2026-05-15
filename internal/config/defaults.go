package config

// Defaults returns the built-in configuration used as the lowest-priority
// layer in Load(). It contains values that let ub start sanely with no
// user configuration at all.
func Defaults() *Config {
	return &Config{
		TUI: TUIConfig{
			Theme: "dark",
		},
		Permissions: PermissionConfig{
			AutoAllowSafe:  true,
			AutoAllowWrite: false,
			AutoAllowExec:  false,
		},
		Context: ContextConfig{
			TriggerRatio:    0.8,
			KeepRecentTurns: 3,
		},
	}
}
