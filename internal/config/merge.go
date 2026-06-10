package config

import "github.com/feimingxliu/ub/internal/reasoning"

// Merge folds layers from lowest precedence (left) to highest (right).
//
// The strategy is a shallow merge of the top-level Config struct:
//
//   - Scalar fields are overwritten when the right-hand layer has a
//     non-zero value (treating zero as "unset" - acceptable for V1).
//   - Map fields (Providers, MCPServers, LSPServers) merge by key, and
//     the value is REPLACED wholesale when both sides have the same
//     key. We deliberately do NOT recurse into ProviderConfig etc.,
//     because partial overrides (api_key from one layer, base_url from
//     another) make debugging harder than re-stating the whole entry.
//   - Struct fields containing scalars only (TUIConfig, ContextConfig,
//     CleanupConfig) are merged field-by-field via dedicated helpers - within
//     those structs we DO use "non-zero wins" except where a field needs a
//     distinct unset value (cleanup.enabled).
//   - Slice fields are replaced wholesale (not appended). None of the
//     current Config fields are top-level slices, but this rule applies
//     to slices inside nested structs (e.g., LSPServerConfig.Args) -
//     which is moot because nested structs are replaced as values.
//   - The Unknown bag is merged map-style: same-key values are replaced.
//
// Nil layers are skipped. The result is always a fresh Config; layers
// are never mutated.
func Merge(layers ...*Config) *Config {
	out := &Config{}
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		mergeInto(out, layer)
	}
	return out
}

func mergeInto(dst, src *Config) {
	if src.DefaultModel != "" {
		dst.DefaultModel = src.DefaultModel
	}
	if src.DefaultProvider != "" {
		dst.DefaultProvider = src.DefaultProvider
	}
	if src.SmallModel != "" {
		dst.SmallModel = src.SmallModel
	}
	if src.ExecutionMode != "" {
		dst.ExecutionMode = src.ExecutionMode
	}
	if src.MaxTurns > 0 {
		dst.MaxTurns = src.MaxTurns
	}
	mergeReasoning(&dst.Reasoning, src.Reasoning)
	mergePrompt(&dst.Prompt, src.Prompt)
	mergeApprovalAgent(&dst.ApprovalAgent, src.ApprovalAgent)
	mergeProviderMap(&dst.Providers, src.Providers)
	mergeProfileMap(&dst.Profiles, src.Profiles)
	if len(src.ToolsDisabled) > 0 {
		dst.ToolsDisabled = append([]string(nil), src.ToolsDisabled...)
	}
	mergeMCPMap(&dst.MCPServers, src.MCPServers)
	mergeLSPMap(&dst.LSPServers, src.LSPServers)
	mergeTUI(&dst.TUI, src.TUI)
	mergePermissions(&dst.Permissions, src.Permissions)
	mergeTools(&dst.Tools, src.Tools)
	mergeContext(&dst.Context, src.Context)
	mergeCleanup(&dst.Cleanup, src.Cleanup)
	mergeHooks(&dst.Hooks, src.Hooks)
	mergeMemory(&dst.Memory, src.Memory)
	mergeUnknown(&dst.Unknown, src.Unknown)
}

func mergeMemory(dst *MemoryConfig, src MemoryConfig) {
	if src.MaxChars != 0 {
		dst.MaxChars = src.MaxChars
	}
	if src.Auto.Enabled != nil {
		enabled := *src.Auto.Enabled
		dst.Auto.Enabled = &enabled
	}
	if src.Auto.Trigger != "" {
		dst.Auto.Trigger = src.Auto.Trigger
	}
	if src.Auto.MaxCandidates != 0 {
		dst.Auto.MaxCandidates = src.Auto.MaxCandidates
	}
	if src.Auto.MaxPromptChars != 0 {
		dst.Auto.MaxPromptChars = src.Auto.MaxPromptChars
	}
	if src.Auto.MinTurnsSinceExtraction != 0 {
		dst.Auto.MinTurnsSinceExtraction = src.Auto.MinTurnsSinceExtraction
	}
	if src.Auto.MinNewMessages != 0 {
		dst.Auto.MinNewMessages = src.Auto.MinNewMessages
	}
	if src.Auto.MinInterval != 0 {
		dst.Auto.MinInterval = src.Auto.MinInterval
	}
	if src.Auto.DrainTimeout != 0 {
		dst.Auto.DrainTimeout = src.Auto.DrainTimeout
	}
	if src.Auto.DisableOnExternalContext != nil {
		disable := *src.Auto.DisableOnExternalContext
		dst.Auto.DisableOnExternalContext = &disable
	}
}

func mergePrompt(dst *PromptConfig, src PromptConfig) {
	mergePromptSection(&dst.WorkspaceInstructions, src.WorkspaceInstructions)
	mergePromptSection(&dst.GitSnapshot, src.GitSnapshot)
	if src.CompactStyle != "" {
		dst.CompactStyle = src.CompactStyle
	}
}

func mergePromptSection(dst *PromptSectionConfig, src PromptSectionConfig) {
	if src.Enabled != nil {
		enabled := *src.Enabled
		dst.Enabled = &enabled
	}
	if src.MaxChars != 0 {
		dst.MaxChars = src.MaxChars
	}
}

// mergeHooks appends each later layer's hook list to the earlier layer's so
// project-level hooks ADD to user-level hooks instead of replacing them. The
// order of execution at runtime follows the slice order.
func mergeHooks(dst *HooksConfig, src HooksConfig) {
	dst.PreToolCall = append(dst.PreToolCall, src.PreToolCall...)
	dst.PostToolCall = append(dst.PostToolCall, src.PostToolCall...)
	dst.PreUserTurn = append(dst.PreUserTurn, src.PreUserTurn...)
	dst.PostUserTurn = append(dst.PostUserTurn, src.PostUserTurn...)
}

func mergeApprovalAgent(dst *ApprovalAgentConfig, src ApprovalAgentConfig) {
	if src.Provider != "" {
		dst.Provider = src.Provider
	}
	if src.Model != "" {
		dst.Model = src.Model
	}
	mergeReasoning(&dst.Reasoning, src.Reasoning)
}

func mergeReasoning(dst *reasoning.Config, src reasoning.Config) {
	if src.Effort != "" {
		dst.Effort = src.Effort
	}
	if src.Summary != "" {
		dst.Summary = src.Summary
	}
}

func mergeProviderMap(dst *map[string]ProviderConfig, src map[string]ProviderConfig) {
	if len(src) == 0 {
		return
	}
	if *dst == nil {
		*dst = make(map[string]ProviderConfig, len(src))
	}
	for k, v := range src {
		(*dst)[k] = v // wholesale value replacement, see Merge doc
	}
}

func mergeProfileMap(dst *map[string]ProfileConfig, src map[string]ProfileConfig) {
	if len(src) == 0 {
		return
	}
	if *dst == nil {
		*dst = make(map[string]ProfileConfig, len(src))
	}
	for k, v := range src {
		(*dst)[k] = v
	}
}

func mergeMCPMap(dst *map[string]MCPServerConfig, src map[string]MCPServerConfig) {
	if len(src) == 0 {
		return
	}
	if *dst == nil {
		*dst = make(map[string]MCPServerConfig, len(src))
	}
	for k, v := range src {
		(*dst)[k] = v
	}
}

func mergeLSPMap(dst *map[string]LSPServerConfig, src map[string]LSPServerConfig) {
	if len(src) == 0 {
		return
	}
	if *dst == nil {
		*dst = make(map[string]LSPServerConfig, len(src))
	}
	for k, v := range src {
		(*dst)[k] = v
	}
}

func mergeTUI(dst *TUIConfig, src TUIConfig) {
	if src.Theme != "" {
		dst.Theme = src.Theme
	}
	if src.Compact {
		dst.Compact = src.Compact
	}
}

func mergePermissions(dst *PermissionConfig, src PermissionConfig) {
	// PermissionConfig is all bools. We treat "true" as the deliberate
	// override; "false" as "leave alone". Users who want to force a
	// permission off in a later layer can still do so by setting an
	// upstream layer to true and the closer one will see the spec
	// override semantics covered in I-20.
	if src.AutoAllowSafe {
		dst.AutoAllowSafe = src.AutoAllowSafe
	}
	if src.AutoAllowWrite {
		dst.AutoAllowWrite = src.AutoAllowWrite
	}
	if src.AutoAllowExec {
		dst.AutoAllowExec = src.AutoAllowExec
	}
}

func mergeTools(dst *ToolsConfig, src ToolsConfig) {
	if src.Job.MaxConcurrent != 0 {
		dst.Job.MaxConcurrent = src.Job.MaxConcurrent
	}
	if src.Job.Retention != 0 {
		dst.Job.Retention = src.Job.Retention
	}
	if src.Job.CleanupInterval != 0 {
		dst.Job.CleanupInterval = src.Job.CleanupInterval
	}
}

func mergeContext(dst *ContextConfig, src ContextConfig) {
	if src.TriggerRatio != 0 {
		dst.TriggerRatio = src.TriggerRatio
	}
	if src.KeepRecentTurns != 0 {
		dst.KeepRecentTurns = src.KeepRecentTurns
	}
	if src.ReserveOutputTokens != 0 {
		dst.ReserveOutputTokens = src.ReserveOutputTokens
	}
	mergeContextToolResults(&dst.ToolResults, src.ToolResults)
}

func mergeContextToolResults(dst *ContextToolResultConfig, src ContextToolResultConfig) {
	if src.InlineMaxBytes != 0 {
		dst.InlineMaxBytes = src.InlineMaxBytes
	}
	if src.InlineMaxLines != 0 {
		dst.InlineMaxLines = src.InlineMaxLines
	}
	if src.SpilloverEnabled != nil {
		enabled := *src.SpilloverEnabled
		dst.SpilloverEnabled = &enabled
	}
	if src.SpilloverMaxAge != 0 {
		dst.SpilloverMaxAge = src.SpilloverMaxAge
	}
	if src.FullMaxBytes != 0 {
		dst.FullMaxBytes = src.FullMaxBytes
	}
	if src.SpilloverDir != "" {
		dst.SpilloverDir = src.SpilloverDir
	}
}

func mergeCleanup(dst *CleanupConfig, src CleanupConfig) {
	if src.Enabled != nil {
		enabled := *src.Enabled
		dst.Enabled = &enabled
	}
	if src.Interval != 0 {
		dst.Interval = src.Interval
	}
	if src.Sessions.MaxAge != 0 {
		dst.Sessions.MaxAge = src.Sessions.MaxAge
	}
	if src.Sessions.MinRecentPerWorkspace != 0 {
		dst.Sessions.MinRecentPerWorkspace = src.Sessions.MinRecentPerWorkspace
	}
	if src.Logs.MaxSizeMB != 0 {
		dst.Logs.MaxSizeMB = src.Logs.MaxSizeMB
	}
	if src.Logs.MaxBackups != 0 {
		dst.Logs.MaxBackups = src.Logs.MaxBackups
	}
}

func mergeUnknown(dst *map[string]any, src map[string]any) {
	if len(src) == 0 {
		return
	}
	if *dst == nil {
		*dst = make(map[string]any, len(src))
	}
	for k, v := range src {
		(*dst)[k] = v
	}
}
