package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAskToolUnmarshalTypeErrorIncludesSchemaHint(t *testing.T) {
	_, err := NewAskTool().Execute(context.Background(), json.RawMessage(`{
		"questions": [{
			"header": "Direction",
			"question": "Which path?",
			"options": ["string1", "string2"]
		}]
	}`))
	if err == nil {
		t.Fatal("expected error for string options")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "cannot unmarshal") {
		t.Fatalf("error missing original unmarshal error: %v", errMsg)
	}
	if !strings.Contains(errMsg, `"label"`) {
		t.Fatalf("error missing schema hint with label: %v", errMsg)
	}
	if !strings.Contains(errMsg, "NOT strings") {
		t.Fatalf("error missing NOT strings hint: %v", errMsg)
	}
}

func TestAskToolMissingHeaderIncludesSchemaHint(t *testing.T) {
	_, err := NewAskTool().Execute(context.Background(), json.RawMessage(`{
		"questions": [{
			"question": "Which path?",
			"options": [{"label": "A"}]
		}]
	}`))
	if err == nil {
		t.Fatal("expected error for missing header")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "questions[0].header is required") {
		t.Fatalf("error missing specific field: %v", errMsg)
	}
	if !strings.Contains(errMsg, `"header"`) {
		t.Fatalf("error missing schema hint: %v", errMsg)
	}
}

func TestAskToolMissingOptionsIncludesSchemaHint(t *testing.T) {
	_, err := NewAskTool().Execute(context.Background(), json.RawMessage(`{
		"questions": [{
			"header": "Direction",
			"question": "Which path?"
		}]
	}`))
	if err == nil {
		t.Fatal("expected error for missing options")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "questions[0].options is required") {
		t.Fatalf("error missing specific field: %v", errMsg)
	}
	if !strings.Contains(errMsg, `"options"`) {
		t.Fatalf("error missing schema hint: %v", errMsg)
	}
}

func TestAskToolValidArgsNoError(t *testing.T) {
	// Non-interactive run returns content but no error.
	result, err := NewAskTool().Execute(context.Background(), json.RawMessage(`{
		"questions": [{
			"header": "Direction",
			"question": "Which path?",
			"options": [{"label": "A"}, {"label": "B"}]
		}]
	}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content == "" {
		t.Fatal("expected non-empty content for valid ask")
	}
	if result.IsError {
		t.Fatal("expected IsError=false for valid ask")
	}
}

func TestFormatAskResponseWithCustomText(t *testing.T) {
	questions := []AskQuestion{{
		Header:   "Backend",
		Question: "Which store should ub use?",
		Options: []AskOption{
			{Label: "SQLite"},
			{Label: "Postgres"},
		},
	}}
	resp := AskResponse{Answers: []AskAnswer{{
		Header:   "Backend",
		Question: "Which store should ub use?",
		Text:     "my custom store",
	}}}
	out := formatAskResponse(resp, questions)
	if !strings.Contains(out, "my custom store") {
		t.Fatalf("expected custom text in response, got: %s", out)
	}
	if strings.Contains(out, "SQLite") || strings.Contains(out, "Postgres") {
		t.Fatalf("custom text answer should not list modeled options, got: %s", out)
	}
	if !strings.Contains(out, "Backend: my custom store") {
		t.Fatalf("expected header-prefixed custom text, got: %s", out)
	}
}

func TestFormatAskResponseCustomTextPrecedenceOverSelected(t *testing.T) {
	// When both Text and Selected are set, Text wins (defensive).
	questions := []AskQuestion{{Header: "H", Question: "Q?", Options: []AskOption{{Label: "A"}}}}
	resp := AskResponse{Answers: []AskAnswer{{
		Header:   "H",
		Question: "Q?",
		Text:     "custom",
		Selected: []AskOption{{Label: "A"}},
	}}}
	out := formatAskResponse(resp, questions)
	if !strings.Contains(out, "custom") {
		t.Fatalf("expected custom text to win, got: %s", out)
	}
	if strings.Contains(out, "A") {
		t.Fatalf("Selected should be ignored when Text present, got: %s", out)
	}
}
