// Package tooloutput limits model-visible tool results and manages spillover
// files for full outputs.
package tooloutput

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/feimingxliu/ub/internal/config"
	"github.com/feimingxliu/ub/internal/tool"
)

const (
	DefaultInlineMaxBytes = 12 * 1024
	DefaultInlineMaxLines = 400
	DefaultReserveTokens  = 12000
	// DefaultFullMaxBytes caps the size of one spillover file. Beyond this,
	// LimitResult truncates the disk artifact (and notes the original size
	// in OriginalBytes so the model still knows how much was discarded).
	DefaultFullMaxBytes = 50 * 1024 * 1024
)

var unsafePathChars = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// Limits is the effective tool-result limiting configuration.
type Limits struct {
	InlineMaxBytes   int
	InlineMaxLines   int
	SpilloverEnabled bool
	// FullMaxBytes caps the on-disk spillover file size. <= 0 means no
	// cap; EffectiveLimits substitutes DefaultFullMaxBytes.
	FullMaxBytes int
	// SpilloverDir, when non-empty, replaces "<stateRoot>/tool_outputs/"
	// as the spillover root. Useful for routing spillover to a different
	// disk (e.g. /var/tmp on a host with a small home).
	SpilloverDir string
}

// LimitOptions identifies one tool result and where spillover files should be
// stored.
type LimitOptions struct {
	SessionID string
	ToolUseID string
	StateRoot string
	Limits    Limits
}

// EffectiveLimits returns normalized limits from the merged config.
func EffectiveLimits(cfg config.ContextConfig) Limits {
	limits := Limits{
		InlineMaxBytes:   cfg.ToolResults.InlineMaxBytes,
		InlineMaxLines:   cfg.ToolResults.InlineMaxLines,
		SpilloverEnabled: true,
		FullMaxBytes:     cfg.ToolResults.FullMaxBytes,
		SpilloverDir:     strings.TrimSpace(cfg.ToolResults.SpilloverDir),
	}
	if limits.InlineMaxBytes <= 0 {
		limits.InlineMaxBytes = DefaultInlineMaxBytes
	}
	if limits.InlineMaxLines <= 0 {
		limits.InlineMaxLines = DefaultInlineMaxLines
	}
	if limits.FullMaxBytes <= 0 {
		limits.FullMaxBytes = DefaultFullMaxBytes
	}
	if cfg.ToolResults.SpilloverEnabled != nil {
		limits.SpilloverEnabled = *cfg.ToolResults.SpilloverEnabled
	}
	return limits
}

// ReserveOutputTokens returns the normalized output-token reserve used when
// deciding whether a request is near the context limit.
func ReserveOutputTokens(cfg config.ContextConfig) int {
	if cfg.ReserveOutputTokens <= 0 {
		return DefaultReserveTokens
	}
	return cfg.ReserveOutputTokens
}

// StateRoot returns ub's user state directory.
func StateRoot() (string, error) {
	if xdg := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); xdg != "" {
		return filepath.Join(xdg, "ub"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "ub"), nil
}

// OutputRoot returns the root directory containing all tool-output spillover
// files.
func OutputRoot(stateRoot string) (string, error) {
	if strings.TrimSpace(stateRoot) == "" {
		var err error
		stateRoot, err = StateRoot()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(stateRoot, "tool_outputs"), nil
}

// SafePathPart sanitizes value into a filesystem-safe path component used by
// the spillover layout. Exported so tools that want to look up an existing
// spillover file (e.g. the tool_result tool) can use the same sanitization
// the writer used.
func SafePathPart(value string) string {
	return safePathPart(value)
}

// SpilloverPath returns the absolute path LimitResult would write to for a
// (sessionID, toolUseID) pair under stateRoot. It does NOT touch disk and is
// safe to call when no spillover file has been written yet. An empty
// stateRoot falls back to the default state root (StateRoot()).
func SpilloverPath(stateRoot, sessionID, toolUseID string) (string, error) {
	root, err := OutputRoot(stateRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, safePathPart(sessionID), safePathPart(toolUseID)+".txt"), nil
}

// LimitResult returns a copy of result whose Content is safe to place back in
// model context. If the visible content is truncated, or the tool supplied a
// separate FullContent value, the full output is written to spillover storage
// when enabled and session metadata is available.
func LimitResult(result tool.Result, opts LimitOptions) (tool.Result, error) {
	full := result.FullContent
	if full == "" {
		full = result.Content
	}
	visible := result.Content
	if visible == "" && full != "" {
		visible = full
	}
	limits := opts.Limits
	if limits.InlineMaxBytes <= 0 {
		limits.InlineMaxBytes = DefaultInlineMaxBytes
	}
	if limits.InlineMaxLines <= 0 {
		limits.InlineMaxLines = DefaultInlineMaxLines
	}

	needsLimit := full != visible || exceedsLimits(visible, limits)
	if !needsLimit {
		result.FullContent = ""
		return result, nil
	}

	originalBytes := len([]byte(full))
	fullPath := ""
	if limits.SpilloverEnabled && strings.TrimSpace(opts.SessionID) != "" && strings.TrimSpace(opts.ToolUseID) != "" {
		toWrite := full
		if limits.FullMaxBytes > 0 && originalBytes > limits.FullMaxBytes {
			kept := validPrefix(full, limits.FullMaxBytes)
			toWrite = kept + fmt.Sprintf("\n... [spillover truncated: original_bytes=%d kept=%d]\n", originalBytes, len(kept))
		}
		path, err := writeSpilloverAt(limits.SpilloverDir, opts.StateRoot, opts.SessionID, opts.ToolUseID, toWrite)
		if err != nil {
			return result, err
		}
		fullPath = path
	}

	footer := truncationFooter(originalBytes, fullPath)
	previewBudget := limits.InlineMaxBytes
	if footer != "" {
		previewBudget -= len([]byte(footer)) + 1
	}
	if previewBudget < 0 {
		previewBudget = 0
	}
	preview := headWithin(visible, Limits{
		InlineMaxBytes:   previewBudget,
		InlineMaxLines:   limits.InlineMaxLines,
		SpilloverEnabled: limits.SpilloverEnabled,
	})
	if preview != "" && footer != "" {
		preview += "\n"
	}
	result.Content = preview + footer
	result.FullContent = ""
	result.Truncated = true
	result.OriginalBytes = originalBytes
	result.FullOutputPath = fullPath
	return result, nil
}

func exceedsLimits(text string, limits Limits) bool {
	return len([]byte(text)) > limits.InlineMaxBytes || lineCount(text) > limits.InlineMaxLines
}

func lineCount(text string) int {
	if text == "" {
		return 0
	}
	count := 1
	for _, r := range text {
		if r == '\n' {
			count++
		}
	}
	return count
}

func headWithin(text string, limits Limits) string {
	if text == "" || limits.InlineMaxBytes <= 0 || limits.InlineMaxLines <= 0 {
		return ""
	}
	var b strings.Builder
	bytesUsed := 0
	linesUsed := 0
	for len(text) > 0 && linesUsed < limits.InlineMaxLines && bytesUsed < limits.InlineMaxBytes {
		segment := text
		if idx := strings.IndexByte(text, '\n'); idx >= 0 {
			segment = text[:idx+1]
		}
		remaining := limits.InlineMaxBytes - bytesUsed
		if len([]byte(segment)) > remaining {
			b.WriteString(validPrefix(segment, remaining))
			break
		}
		b.WriteString(segment)
		bytesUsed += len([]byte(segment))
		linesUsed++
		text = text[len(segment):]
	}
	return strings.TrimRight(b.String(), "\n")
}

func validPrefix(text string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	raw := []byte(text)
	if len(raw) <= maxBytes {
		return text
	}
	raw = raw[:maxBytes]
	for len(raw) > 0 && !utf8.Valid(raw) {
		raw = raw[:len(raw)-1]
	}
	return string(raw)
}

func truncationFooter(originalBytes int, fullPath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "... [tool result truncated: original_bytes=%d]", originalBytes)
	if fullPath != "" {
		fmt.Fprintf(&b, "\nfull_output_path=%s", fullPath)
		b.WriteString("\nUse the read tool with this absolute path plus offset/limit to inspect more.")
	}
	return b.String()
}

// writeSpilloverAt prefers spilloverDir when non-empty; otherwise it falls
// back to <stateRoot>/tool_outputs via SpilloverPath. Either way the inner
// layout is <root>/<safe sessionID>/<safe toolUseID>.txt.
func writeSpilloverAt(spilloverDir, stateRoot, sessionID, toolUseID, content string) (string, error) {
	var path string
	if strings.TrimSpace(spilloverDir) != "" {
		path = filepath.Join(spilloverDir, safePathPart(sessionID), safePathPart(toolUseID)+".txt")
	} else {
		var err error
		path, err = SpilloverPath(stateRoot, sessionID, toolUseID)
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create tool output directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("write tool output spillover: %w", err)
	}
	return path, nil
}

func safePathPart(value string) string {
	value = strings.Trim(unsafePathChars.ReplaceAllString(strings.TrimSpace(value), "_"), "_")
	if value == "" {
		return "unnamed"
	}
	if len(value) > 120 {
		value = value[:120]
	}
	return value
}
