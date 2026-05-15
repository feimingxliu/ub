package tool

import (
	"fmt"
	"sort"

	"github.com/invopop/jsonschema"
)

// Registry stores Tool instances keyed by Name().
//
// Registry is intentionally not safe for concurrent registration: callers
// register the full tool set during startup, then hand a read-only view
// to the agent runtime. Concurrent reads of Get/All/Schemas are safe as
// long as no Register happens in parallel.
type Registry struct {
	tools map[string]Tool
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

// Register adds t to the registry. Returns a non-nil error if a tool
// with the same Name() is already registered; in that case the original
// instance is kept.
func (r *Registry) Register(t Tool) error {
	if t == nil {
		return fmt.Errorf("tool: register nil tool")
	}
	name := t.Name()
	if name == "" {
		return fmt.Errorf("tool: register tool with empty name")
	}
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool: %q already registered", name)
	}
	r.tools[name] = t
	return nil
}

// Get returns the tool registered under name. The boolean reports
// whether the lookup hit.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// All returns all registered tools in stable order (ascending by name).
func (r *Registry) All() []Tool {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Tool, len(names))
	for i, name := range names {
		out[i] = r.tools[name]
	}
	return out
}

// Schemas returns the input JSON Schema for every registered tool,
// keyed by tool name. The keys are exactly the names returned by All().
func (r *Registry) Schemas() map[string]*jsonschema.Schema {
	out := make(map[string]*jsonschema.Schema, len(r.tools))
	for name, t := range r.tools {
		out[name] = t.Schema()
	}
	return out
}
