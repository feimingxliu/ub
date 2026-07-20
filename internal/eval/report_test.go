package eval

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderJSONAndText(t *testing.T) {
	maxContext, triggerRatio, keepTurns := 30000, 0.5, 1
	report := Report{Task: "sample", Passed: false, FailureCategory: FailureAssertion, Failure: "failed", Runtime: Runtime{MaxContextTokens: &maxContext, Context: RuntimeContext{TriggerRatio: &triggerRatio, KeepRecentTurns: &keepTurns}}, Metrics: Metrics{Turns: 2, ToolCalls: []string{"read"}, ContextDecisions: []ContextDecision{{Action: "compact", Reason: "threshold"}}}, Assertions: []AssertionResult{{Name: "file", Passed: false, Message: "missing"}}, Workspace: "/tmp/eval"}
	var jsonOut bytes.Buffer
	if err := RenderJSON(&jsonOut, report); err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(jsonOut.Bytes(), &decoded); err != nil || decoded.Task != "sample" {
		t.Fatalf("json decode=%#v err=%v output=%s", decoded, err, jsonOut.String())
	}
	var textOut bytes.Buffer
	if err := RenderText(&textOut, report); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Eval FAIL: sample", "Runtime: max_context_tokens=30000 trigger_ratio=0.5 keep_recent_turns=1", "Context: compact(threshold)", "✗ file: missing", "Workspace: /tmp/eval"} {
		if !strings.Contains(textOut.String(), want) {
			t.Errorf("text output missing %q:\n%s", want, textOut.String())
		}
	}
}
