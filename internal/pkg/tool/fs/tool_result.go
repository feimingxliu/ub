package fs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/pkg/tool"
	"github.com/feimingxliu/ub/internal/pkg/workspace/tooloutput"
)

type toolResultArgs struct {
	ToolUseID string      `json:"tool_use_id" jsonschema:"required,description=The tool_use_id of a previous tool call whose output was spilled to disk. Visible in rollout events and as full_output_path footers."`
	Offset    tool.IntArg `json:"offset,omitempty" jsonschema:"description=1-based line number to start at. Defaults to 1."`
	Limit     tool.IntArg `json:"limit,omitempty"  jsonschema:"description=Maximum number of lines to return. Defaults to all lines (capped for model context)."`
}

type toolResultTool struct {
	outputRoot string
	maxLines   int
	schema     *jsonschema.Schema
}

func newToolResultTool(outputRoot string, maxLines int) *toolResultTool {
	if maxLines <= 0 {
		maxLines = defaultReadMaxLines
	}
	return &toolResultTool{
		outputRoot: outputRoot,
		maxLines:   maxLines,
		schema:     jsonschema.Reflect(&toolResultArgs{}),
	}
}

func (t *toolResultTool) Name() string { return "tool_result" }
func (t *toolResultTool) Description() string {
	return "Read the full output of a previous tool call from spillover storage. Use this when a prior tool result was truncated and you need to inspect content past the inline preview."
}
func (t *toolResultTool) Schema() *jsonschema.Schema { return t.schema }
func (t *toolResultTool) Risk() tool.Risk            { return tool.RiskSafe }

func (t *toolResultTool) Execute(ctx context.Context, raw json.RawMessage) (tool.Result, error) {
	var a toolResultArgs
	if err := tool.UnmarshalArgs(raw, &a); err != nil {
		return tool.Result{}, fmt.Errorf("tool_result: invalid args: %w", err)
	}
	if strings.TrimSpace(a.ToolUseID) == "" {
		return tool.Result{}, fmt.Errorf("tool_result: tool_use_id is required")
	}
	sessionID := tool.SessionIDFromContext(ctx)
	if sessionID == "" {
		return tool.Result{}, fmt.Errorf("tool_result: session id missing from context; the tool can only be invoked by an agent run")
	}
	if strings.TrimSpace(t.outputRoot) == "" {
		return tool.Result{}, fmt.Errorf("tool_result: spillover output root not configured")
	}
	path := filepath.Join(t.outputRoot, tooloutput.SafePathPart(sessionID), tooloutput.SafePathPart(a.ToolUseID)+".txt")
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return tool.Result{}, fmt.Errorf("tool_result: %s not found or output was not spilled", a.ToolUseID)
		}
		return tool.Result{}, fmt.Errorf("tool_result: stat %s: %w", a.ToolUseID, err)
	}
	return readNumberedLines(path, int(a.Offset), int(a.Limit), t.maxLines, "tool_result", a.ToolUseID)
}
