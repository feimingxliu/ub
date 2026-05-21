package context

import (
	"encoding/json"
	"testing"

	"github.com/feimingxliu/ub/internal/message"
	"github.com/feimingxliu/ub/internal/provider"
)

type countingEncoder struct{}

func (countingEncoder) Encode(text string) int {
	if text == "" {
		return 0
	}
	return len([]rune(text))
}

func TestEstimateEmptyMessages(t *testing.T) {
	resetForTest(t)
	if got := Estimate(nil, "gpt-4o"); got != 0 {
		t.Fatalf("Estimate(nil) = %d, want 0", got)
	}
}

func TestEstimateUsesOpenAIEncoderForKnownString(t *testing.T) {
	resetForTest(t)
	loadOpenAIEncoder = func(model string) (textEncoder, error) {
		if model != "gpt-4o" {
			t.Fatalf("model = %q, want gpt-4o", model)
		}
		return countingEncoder{}, nil
	}
	msgs := []message.Message{message.Text(message.RoleUser, "hello world")}
	if got := Estimate(msgs, "openai/gpt-4o"); got != 28 {
		t.Fatalf("Estimate(hello world) = %d, want 28", got)
	}
}

func TestEstimateFallbackUnknownModelIsDeterministic(t *testing.T) {
	resetForTest(t)
	msgs := []message.Message{message.Text(message.RoleUser, "hello world")}
	first := Estimate(msgs, "local/unknown")
	second := Estimate(msgs, "local/unknown")
	if first <= 0 {
		t.Fatalf("Estimate unknown = %d, want > 0", first)
	}
	if second != first {
		t.Fatalf("Estimate unknown second = %d, want %d", second, first)
	}
}

func TestEstimateCountsToolBlocks(t *testing.T) {
	resetForTest(t)
	empty := Estimate([]message.Message{message.New(message.RoleAssistant)}, "local/unknown")
	input, err := json.Marshal(map[string]string{"path": "main.go"})
	if err != nil {
		t.Fatal(err)
	}
	withTools := Estimate([]message.Message{
		message.New(
			message.RoleAssistant,
			message.ToolUseBlock("call-1", "read", input),
			message.ToolResultBlock("call-1", "package main", false),
		),
	}, "local/unknown")
	if withTools <= empty {
		t.Fatalf("tool estimate = %d, empty = %d, want tool estimate larger", withTools, empty)
	}
}

func TestEstimateRequestIncludesToolSchemas(t *testing.T) {
	resetForTest(t)
	msgs := []message.Message{message.Text(message.RoleUser, "hello")}
	withoutTools := EstimateRequest(msgs, nil, "local/unknown")
	withTools := EstimateRequest(msgs, []provider.ToolDefinition{{
		Name:        "read",
		Description: "Read a file",
		Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
	}}, "local/unknown")
	if withTools <= withoutTools {
		t.Fatalf("with tools = %d, without tools = %d", withTools, withoutTools)
	}
	breakdown := EstimateRequestBreakdown(msgs, []provider.ToolDefinition{{
		Name:   "bash",
		Schema: json.RawMessage(`{"type":"object"}`),
	}}, "local/unknown")
	if breakdown.ToolSchema <= 0 || breakdown.Total <= withoutTools {
		t.Fatalf("breakdown = %#v", breakdown)
	}
}

func TestObserveUsageCalibratesFutureEstimates(t *testing.T) {
	resetForTest(t)
	msgs := []message.Message{message.Text(message.RoleUser, "calibrate this")}
	before := Estimate(msgs, "local/model")
	ObserveUsage("local/model", before, before*2)
	after := Estimate(msgs, "local/model")
	if after <= before {
		t.Fatalf("after calibration = %d, before = %d, want larger", after, before)
	}
	otherModel := Estimate(msgs, "local/other")
	if otherModel != before {
		t.Fatalf("other model estimate = %d, want unchanged %d", otherModel, before)
	}
}

func TestObserveUsageIgnoresInvalidValues(t *testing.T) {
	resetForTest(t)
	msgs := []message.Message{message.Text(message.RoleUser, "unchanged")}
	before := Estimate(msgs, "local/model")
	ObserveUsage("local/model", 0, 100)
	ObserveUsage("local/model", before, 0)
	if after := Estimate(msgs, "local/model"); after != before {
		t.Fatalf("after invalid observations = %d, want %d", after, before)
	}
}

func resetForTest(t *testing.T) {
	t.Helper()
	encoderMu.Lock()
	encoderCache = map[string]cachedEncoder{}
	loadOpenAIEncoder = loadTiktokenEncoder
	encoderMu.Unlock()

	calibrationMu.Lock()
	calibrationByModel = map[string]float64{}
	calibrationMu.Unlock()

	t.Cleanup(func() {
		encoderMu.Lock()
		encoderCache = map[string]cachedEncoder{}
		loadOpenAIEncoder = loadTiktokenEncoder
		encoderMu.Unlock()
		calibrationMu.Lock()
		calibrationByModel = map[string]float64{}
		calibrationMu.Unlock()
	})
}
