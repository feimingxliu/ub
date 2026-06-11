package tool_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/invopop/jsonschema"

	"github.com/feimingxliu/ub/internal/pkg/tool"
)

// stubArgs is the JSON schema source for stubTool / previewTool.
type stubArgs struct {
	Path string `json:"path"`
}

type stubTool struct {
	name string
	risk tool.Risk
}

func (t *stubTool) Name() string               { return t.name }
func (t *stubTool) Description() string        { return "stub for tests" }
func (t *stubTool) Schema() *jsonschema.Schema { return jsonschema.Reflect(&stubArgs{}) }
func (t *stubTool) Risk() tool.Risk            { return t.risk }
func (t *stubTool) Execute(_ context.Context, _ json.RawMessage) (tool.Result, error) {
	return tool.Result{Content: t.name}, nil
}

type previewTool struct {
	stubTool
}

func (t *previewTool) Preview(_ context.Context, _ json.RawMessage) (tool.Preview, error) {
	return tool.Preview{
		Summary: "preview " + t.name,
		Files: []tool.FileDiff{{
			Path:        "a.go",
			Kind:        tool.KindModify,
			UnifiedDiff: "@@",
		}},
	}, nil
}

// Compile-time assertion that stubTool implements Tool.
var _ tool.Tool = (*stubTool)(nil)

// Compile-time assertion that previewTool implements PreviewableTool.
var _ tool.PreviewableTool = (*previewTool)(nil)

func TestTool_SchemaMarshalsJSON(t *testing.T) {
	s := (&stubTool{name: "stub", risk: tool.RiskSafe}).Schema()
	buf, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	if len(buf) == 0 || buf[0] != '{' {
		t.Fatalf("schema JSON looks invalid: %s", buf)
	}
}

func TestPreviewableTool_TypeAssertion(t *testing.T) {
	var pv tool.Tool = &previewTool{stubTool: stubTool{name: "edit", risk: tool.RiskWrite}}
	if _, ok := pv.(tool.PreviewableTool); !ok {
		t.Fatalf("expected previewTool to satisfy PreviewableTool")
	}

	var plain tool.Tool = &stubTool{name: "read", risk: tool.RiskSafe}
	if _, ok := plain.(tool.PreviewableTool); ok {
		t.Fatalf("plain stubTool MUST NOT satisfy PreviewableTool")
	}
}

func TestPreviewAndResult_JSONFieldNames(t *testing.T) {
	preview := tool.Preview{
		Summary: "Write a.go",
		Files: []tool.FileDiff{{
			Path:        "a.go",
			Kind:        tool.KindCreate,
			UnifiedDiff: "@@ -0,0 +1 @@\n+hi\n",
		}},
	}
	pb, err := json.Marshal(preview)
	if err != nil {
		t.Fatalf("marshal preview: %v", err)
	}
	for _, want := range []string{`"summary"`, `"files"`, `"path"`, `"kind"`, `"unified_diff"`} {
		if !strings.Contains(string(pb), want) {
			t.Errorf("preview JSON missing %s: %s", want, pb)
		}
	}

	res := tool.Result{
		Content: "ok",
		IsError: true,
		Files: []tool.FileChange{{
			Path: "a.go",
			Kind: tool.KindModify,
		}},
	}
	rb, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	for _, want := range []string{`"content"`, `"is_error"`, `"files"`, `"path"`, `"kind"`} {
		if !strings.Contains(string(rb), want) {
			t.Errorf("result JSON missing %s: %s", want, rb)
		}
	}
}

func TestRisk_Values(t *testing.T) {
	cases := []tool.Risk{tool.RiskSafe, tool.RiskWrite, tool.RiskExec}
	if len(cases) != 3 {
		t.Fatalf("expected three risk values, got %d", len(cases))
	}
	for _, r := range cases {
		if r == "" {
			t.Errorf("risk constant has empty value")
		}
	}
}
