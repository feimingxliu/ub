package tool_test

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/feimingxliu/ub/internal/tool"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := tool.New()
	read := &stubTool{name: "read", risk: tool.RiskSafe}
	if err := reg.Register(read); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := reg.Get("read")
	if !ok {
		t.Fatalf("Get(read) MUST hit after registration")
	}
	if got != read {
		t.Fatalf("Get(read) returned a different instance")
	}
}

func TestRegistry_DuplicateNameReturnsError(t *testing.T) {
	reg := tool.New()
	first := &stubTool{name: "read", risk: tool.RiskSafe}
	if err := reg.Register(first); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	dup := &stubTool{name: "read", risk: tool.RiskSafe}
	if err := reg.Register(dup); err == nil {
		t.Fatalf("duplicate Register MUST return error")
	}
	got, ok := reg.Get("read")
	if !ok {
		t.Fatalf("Get(read) MUST still hit after duplicate Register")
	}
	if got != first {
		t.Fatalf("duplicate Register replaced the original instance")
	}
}

func TestRegistry_RegisterNilOrEmpty(t *testing.T) {
	reg := tool.New()
	if err := reg.Register(nil); err == nil {
		t.Fatalf("Register(nil) MUST return error")
	}
	if err := reg.Register(&stubTool{name: ""}); err == nil {
		t.Fatalf("Register with empty name MUST return error")
	}
}

func TestRegistry_GetMissing(t *testing.T) {
	reg := tool.New()
	if got, ok := reg.Get("ghost"); ok || got != nil {
		t.Fatalf("Get(ghost) on empty Registry MUST return (nil, false), got (%v, %v)", got, ok)
	}
}

func TestRegistry_AllSortedByName(t *testing.T) {
	reg := tool.New()
	for _, name := range []string{"write", "read", "ls"} {
		if err := reg.Register(&stubTool{name: name, risk: tool.RiskSafe}); err != nil {
			t.Fatalf("Register %s: %v", name, err)
		}
	}
	all := reg.All()
	names := make([]string, len(all))
	for i, tl := range all {
		names[i] = tl.Name()
	}
	want := []string{"ls", "read", "write"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("All() not sorted: got %v want %v", names, want)
	}
}

func TestRegistry_SchemasMirrorsAll(t *testing.T) {
	reg := tool.New()
	tools := []*stubTool{
		{name: "alpha", risk: tool.RiskSafe},
		{name: "beta", risk: tool.RiskSafe},
	}
	for _, tl := range tools {
		if err := reg.Register(tl); err != nil {
			t.Fatalf("Register %s: %v", tl.Name(), err)
		}
	}
	schemas := reg.Schemas()

	all := reg.All()
	if len(schemas) != len(all) {
		t.Fatalf("Schemas/All length mismatch: %d vs %d", len(schemas), len(all))
	}

	gotNames := make([]string, 0, len(schemas))
	for k := range schemas {
		gotNames = append(gotNames, k)
	}
	sort.Strings(gotNames)

	wantNames := make([]string, len(all))
	for i, tl := range all {
		wantNames[i] = tl.Name()
	}

	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("Schemas keys != All names: %v vs %v", gotNames, wantNames)
	}

	for _, tl := range all {
		fromMap, err := json.Marshal(schemas[tl.Name()])
		if err != nil {
			t.Fatalf("marshal schema map[%s]: %v", tl.Name(), err)
		}
		fromTool, err := json.Marshal(tl.Schema())
		if err != nil {
			t.Fatalf("marshal tool.Schema(%s): %v", tl.Name(), err)
		}
		if string(fromMap) != string(fromTool) {
			t.Errorf("Schemas[%s] JSON differs from tool.Schema():\n map=%s\ntool=%s",
				tl.Name(), fromMap, fromTool)
		}
	}
}
