package mcp

import (
	"encoding/json"
	"testing"
)

func TestCallResultTextJoinsTextAndDataBlocks(t *testing.T) {
	result := CallResult{Content: []ContentBlock{
		{Type: "text", Text: "first"},
		{Type: "image"},
		{Type: "blob", Data: json.RawMessage(`{"kind":"raw"}`)},
		{Type: "text", Text: "last"},
	}}

	if got, want := result.Text(), "first\n{\"kind\":\"raw\"}\nlast"; got != want {
		t.Fatalf("Text() = %q, want %q", got, want)
	}
}
