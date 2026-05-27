package tool

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIntArgAcceptsIntegerStrings(t *testing.T) {
	var got struct {
		Count IntArg `json:"count"`
	}
	if err := json.Unmarshal([]byte(`{"count":"42"}`), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if int(got.Count) != 42 {
		t.Fatalf("count = %d, want 42", got.Count)
	}
}

func TestBoolArgAcceptsBooleanStrings(t *testing.T) {
	var got struct {
		All BoolArg `json:"all"`
	}
	if err := json.Unmarshal([]byte(`{"all":"false"}`), &got); err != nil {
		t.Fatalf("unmarshal false: %v", err)
	}
	if bool(got.All) {
		t.Fatalf("all = true, want false")
	}
	if err := json.Unmarshal([]byte(`{"all":"true"}`), &got); err != nil {
		t.Fatalf("unmarshal true: %v", err)
	}
	if !bool(got.All) {
		t.Fatalf("all = false, want true")
	}
}

func TestBoolArgRejectsNonBooleanStrings(t *testing.T) {
	var got struct {
		All BoolArg `json:"all"`
	}
	err := json.Unmarshal([]byte(`{"all":"yes"}`), &got)
	if err == nil || !strings.Contains(err.Error(), "expected boolean") {
		t.Fatalf("expected boolean error, got %v", err)
	}
}

func TestUnmarshalArgsAcceptsJSONEncodedObjectString(t *testing.T) {
	var got struct {
		Count IntArg  `json:"count"`
		All   BoolArg `json:"all"`
	}
	raw := json.RawMessage(`"{\"count\":\"42\",\"all\":\"true\"}"`)
	if err := UnmarshalArgs(raw, &got); err != nil {
		t.Fatalf("UnmarshalArgs: %v", err)
	}
	if int(got.Count) != 42 || !bool(got.All) {
		t.Fatalf("decoded args = %#v, want count=42 all=true", got)
	}
}
