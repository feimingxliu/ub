package agent

import (
	"strings"
	"unicode/utf8"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/message"
	execmode "github.com/feimingxliu/ub/internal/mode"
	contextmgr "github.com/feimingxliu/ub/internal/tokenizer"
)

const (
	promptSectionCodingAgent           = "coding_agent"
	promptSectionRuntime               = "runtime"
	promptSectionWorkspaceInstructions = "workspace_instructions"
	promptSectionGitSnapshot           = "git_snapshot"
	promptSectionExecutionMode         = "execution_mode"
	promptSectionMemory                = "memory"
	promptSectionCompactInstructions   = "compact_instructions"
)

const (
	promptStatusIncluded    = "included"
	promptStatusDisabled    = "disabled"
	promptStatusUnavailable = "unavailable"
	promptStatusOmitted     = "omitted"
)

const (
	promptStabilityStable  = "stable"
	promptStabilityDynamic = "dynamic"
)

const (
	promptVariantMain    = "main"
	promptVariantNoTool  = "no-tool"
	promptVariantCompact = "compact"
)

// PromptSectionManifest describes one known prompt section without exposing
// its content unless the caller explicitly requests it.
type PromptSectionManifest struct {
	ID              string `json:"id"`
	Position        int    `json:"position"`
	Role            string `json:"role"`
	Status          string `json:"status"`
	Stability       string `json:"stability"`
	Source          string `json:"source"`
	Chars           int    `json:"chars"`
	EstimatedTokens int    `json:"estimated_tokens"`
	Truncated       bool   `json:"truncated"`
	Content         string `json:"content,omitempty"`
}

// PromptManifest is the inspectable description of one provider prompt
// prefix. EstimatedTokens is diagnostic and is not a billing guarantee.
type PromptManifest struct {
	Variant         string                  `json:"variant"`
	Model           string                  `json:"model,omitempty"`
	ToolsEnabled    bool                    `json:"tools_enabled"`
	TotalChars      int                     `json:"total_chars"`
	EstimatedTokens int                     `json:"estimated_tokens"`
	Sections        []PromptSectionManifest `json:"sections"`
}

// PromptInspectOptions selects the local prompt prefix rendered by
// InspectPrompt. Model only affects token estimation.
type PromptInspectOptions struct {
	Runtime        RuntimeContext
	WorkspaceRoot  string
	Prompt         config.PromptConfig
	Mode           execmode.Mode
	MemoryMaxChars int
	Model          string
	Variant        string
	ShowContent    bool
}

type promptSection struct {
	id        string
	role      message.Role
	status    string
	stability string
	source    string
	message   message.Message
	truncated bool
}

type promptRegistry struct {
	startup        []promptSection
	runtime        RuntimeContext
	workspaceRoot  string
	memoryMaxChars int
}

func newPromptRegistry(runtime RuntimeContext, workspaceRoot string, cfg config.PromptConfig, memoryMaxChars int) *promptRegistry {
	return newPromptRegistryWithGit(runtime, workspaceRoot, cfg, memoryMaxChars, realGitCommand)
}

func newPromptRegistryWithGit(runtime RuntimeContext, workspaceRoot string, cfg config.PromptConfig, memoryMaxChars int, run gitCommandRunner) *promptRegistry {
	runtime = runtime.normalized()
	return &promptRegistry{
		startup:        buildStartupPromptSections(runtime, workspaceRoot, cfg, run),
		runtime:        runtime,
		workspaceRoot:  strings.TrimSpace(workspaceRoot),
		memoryMaxChars: memoryMaxChars,
	}
}

func newNoToolPromptRegistry(runtime RuntimeContext, workspaceRoot string, memoryMaxChars int) *promptRegistry {
	return &promptRegistry{
		runtime:        runtime.normalized(),
		workspaceRoot:  strings.TrimSpace(workspaceRoot),
		memoryMaxChars: memoryMaxChars,
	}
}

func buildStartupPromptSections(runtime RuntimeContext, workspaceRoot string, cfg config.PromptConfig, run gitCommandRunner) []promptSection {
	cfg = effectivePromptConfig(cfg)
	sections := []promptSection{
		includedPromptSection(promptSectionCodingAgent, promptStabilityStable, "builtin", codingAgentInstructionsMessage(), false),
	}
	if msg, ok := runtime.message(); ok {
		sections = append(sections, includedPromptSection(promptSectionRuntime, promptStabilityStable, "environment", msg, false))
	} else {
		sections = append(sections, emptyPromptSection(promptSectionRuntime, promptStatusUnavailable, promptStabilityStable, "environment"))
	}
	sections = append(sections, workspacePromptSection(workspaceRoot, cfg.WorkspaceInstructions))
	sections = append(sections, gitPromptSection(workspaceRoot, cfg.GitSnapshot, run))
	return sections
}

func workspacePromptSection(workspaceRoot string, cfg config.PromptSectionConfig) promptSection {
	if !promptSectionEnabled(cfg.Enabled) {
		return emptyPromptSection(promptSectionWorkspaceInstructions, promptStatusDisabled, promptStabilityStable, "AGENTS.md")
	}
	msg, ok := workspaceInstructionsMessage(workspaceRoot, cfg)
	if !ok {
		return emptyPromptSection(promptSectionWorkspaceInstructions, promptStatusUnavailable, promptStabilityStable, "AGENTS.md")
	}
	return includedPromptSection(promptSectionWorkspaceInstructions, promptStabilityStable, "AGENTS.md", msg, strings.Contains(msg.Text(), "[workspace instructions truncated]"))
}

func gitPromptSection(workspaceRoot string, cfg config.PromptSectionConfig, run gitCommandRunner) promptSection {
	if !promptSectionEnabled(cfg.Enabled) {
		return emptyPromptSection(promptSectionGitSnapshot, promptStatusDisabled, promptStabilityDynamic, "git")
	}
	msg, ok := gitSnapshotMessage(workspaceRoot, cfg, run)
	if !ok {
		return emptyPromptSection(promptSectionGitSnapshot, promptStatusUnavailable, promptStabilityDynamic, "git")
	}
	return includedPromptSection(promptSectionGitSnapshot, promptStabilityDynamic, "git", msg, strings.Contains(msg.Text(), "[git snapshot truncated]"))
}

func executionModePromptSection(mode execmode.Mode) promptSection {
	msg, ok := executionModeMessageForMode(mode)
	if !ok {
		return emptyPromptSection(promptSectionExecutionMode, promptStatusUnavailable, promptStabilityDynamic, "execution-mode")
	}
	return includedPromptSection(promptSectionExecutionMode, promptStabilityDynamic, "execution-mode", msg, false)
}

func memoryPromptSection(workspaceRoot string, maxChars int) promptSection {
	msg, ok := memoryMessageForWorkspace(workspaceRoot, maxChars)
	if !ok {
		return emptyPromptSection(promptSectionMemory, promptStatusUnavailable, promptStabilityDynamic, "workspace-memory")
	}
	return includedPromptSection(promptSectionMemory, promptStabilityDynamic, "workspace-memory", msg, strings.Contains(msg.Text(), "[memory truncated]"))
}

func includedPromptSection(id, stability, source string, msg message.Message, truncated bool) promptSection {
	return promptSection{
		id:        id,
		role:      msg.Role,
		status:    promptStatusIncluded,
		stability: stability,
		source:    source,
		message:   msg,
		truncated: truncated,
	}
}

func emptyPromptSection(id, status, stability, source string) promptSection {
	return promptSection{
		id:        id,
		role:      message.RoleSystem,
		status:    status,
		stability: stability,
		source:    source,
	}
}

func (r *promptRegistry) mainSections(mode execmode.Mode) []promptSection {
	if r == nil {
		return nil
	}
	sections := clonePromptSections(r.startup)
	sections = append(sections, executionModePromptSection(mode))
	sections = append(sections, memoryPromptSection(r.workspaceRoot, r.memoryMaxChars))
	return sections
}

func (r *promptRegistry) noToolSections() []promptSection {
	if r == nil {
		return nil
	}
	sections := []promptSection{
		emptyPromptSection(promptSectionCodingAgent, promptStatusOmitted, promptStabilityStable, "builtin"),
	}
	if msg, ok := r.runtime.noToolMessage(); ok {
		sections = append(sections, includedPromptSection(promptSectionRuntime, promptStabilityStable, "environment", msg, false))
	} else {
		sections = append(sections, emptyPromptSection(promptSectionRuntime, promptStatusUnavailable, promptStabilityStable, "environment"))
	}
	sections = append(
		sections,
		emptyPromptSection(promptSectionWorkspaceInstructions, promptStatusOmitted, promptStabilityStable, "AGENTS.md"),
		emptyPromptSection(promptSectionGitSnapshot, promptStatusOmitted, promptStabilityDynamic, "git"),
		emptyPromptSection(promptSectionExecutionMode, promptStatusOmitted, promptStabilityDynamic, "execution-mode"),
		memoryPromptSection(r.workspaceRoot, r.memoryMaxChars),
	)
	return sections
}

func compactPromptSections(cfg config.PromptConfig) []promptSection {
	template := summaryPromptTemplate
	if strings.TrimSpace(cfg.CompactStyle) == config.CompactStyleShort {
		template = summaryShortPromptTemplate
	}
	return []promptSection{
		includedPromptSection(promptSectionCompactInstructions, promptStabilityStable, "builtin", message.Text(message.RoleUser, template), false),
	}
}

func clonePromptSections(in []promptSection) []promptSection {
	if in == nil {
		return nil
	}
	out := make([]promptSection, len(in))
	for i, section := range in {
		out[i] = section
		out[i].message = section.message.Clone()
	}
	return out
}

func promptMessages(sections []promptSection) []message.Message {
	out := make([]message.Message, 0, len(sections))
	for _, section := range sections {
		if section.status == promptStatusIncluded {
			out = append(out, section.message.Clone())
		}
	}
	return out
}

func promptManifest(variant, model string, sections []promptSection, showContent bool) PromptManifest {
	model = strings.TrimSpace(model)
	manifest := PromptManifest{
		Variant:      variant,
		Model:        model,
		ToolsEnabled: variant != promptVariantCompact && variant != promptVariantNoTool,
		Sections:     make([]PromptSectionManifest, 0, len(sections)),
	}
	for i, section := range sections {
		item := PromptSectionManifest{
			ID:        section.id,
			Position:  i + 1,
			Role:      string(section.role),
			Status:    section.status,
			Stability: section.stability,
			Source:    section.source,
			Truncated: section.truncated,
		}
		if section.status == promptStatusIncluded {
			content := section.message.Text()
			item.Chars = utf8.RuneCountInString(content)
			item.EstimatedTokens = contextmgr.Estimate([]message.Message{section.message}, model)
			manifest.TotalChars += item.Chars
			if showContent {
				item.Content = content
			}
		}
		manifest.Sections = append(manifest.Sections, item)
	}
	manifest.EstimatedTokens = contextmgr.Estimate(promptMessages(sections), model)
	return manifest
}

// InspectPrompt builds a local prompt manifest without invoking a provider or
// creating any session state. Variant supports main and compact; unknown
// variants fall back to main for backwards-compatible direct API callers.
func InspectPrompt(opts PromptInspectOptions) PromptManifest {
	if strings.EqualFold(strings.TrimSpace(opts.Variant), promptVariantCompact) {
		return promptManifest(promptVariantCompact, opts.Model, compactPromptSections(opts.Prompt), opts.ShowContent)
	}
	registry := newPromptRegistry(opts.Runtime, opts.WorkspaceRoot, opts.Prompt, opts.MemoryMaxChars)
	return promptManifest(promptVariantMain, opts.Model, registry.mainSections(opts.Mode), opts.ShowContent)
}
