package lsp

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
)

// flattenHoverContents normalizes the three legal shapes of Hover.contents
// (MarkupContent object, MarkedString object, MarkedString[]) into a single
// human-facing string. Empty results yield "".
func flattenHoverContents(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	if mc, ok := tryMarkupContent(raw); ok {
		return mc, nil
	}
	if ms, ok := tryMarkedString(raw); ok {
		return ms, nil
	}
	// Array form.
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		var b strings.Builder
		for _, item := range arr {
			if mc, ok := tryMarkupContent(item); ok {
				if b.Len() > 0 {
					b.WriteString("\n\n")
				}
				b.WriteString(mc)
				continue
			}
			if ms, ok := tryMarkedString(item); ok {
				if b.Len() > 0 {
					b.WriteString("\n\n")
				}
				b.WriteString(ms)
			}
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("lsp: unrecognized hover contents shape: %s", string(raw))
}

func tryMarkupContent(raw json.RawMessage) (string, bool) {
	var obj struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil || obj.Value == "" {
		return "", false
	}
	if obj.Kind == "" {
		// MarkedString {language, value} also has Value but no Kind; treat
		// that as not-MarkupContent and let tryMarkedString handle it.
		return "", false
	}
	return obj.Value, true
}

func tryMarkedString(raw json.RawMessage) (string, bool) {
	// MarkedString is either a plain string or {language, value}.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		return s, true
	}
	var obj struct {
		Language string `json:"language"`
		Value    string `json:"value"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil || obj.Value == "" {
		return "", false
	}
	return obj.Value, true
}

// parseCompletion normalizes the two completion response shapes
// (CompletionList vs CompletionItem[]) into a flat slice.
func parseCompletion(raw json.RawMessage) ([]CompletionItem, error) {
	var arr []CompletionItem
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}
	var list struct {
		IsIncomplete bool             `json:"isIncomplete,omitempty"`
		Items        []CompletionItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("lsp: decode completion: %w", err)
	}
	return list.Items, nil
}

// parseDocumentSymbols handles the hierarchical DocumentSymbol[] response.
// SymbolInformation[] (the older flat shape) is also accepted: each item is
// promoted to a leaf DocumentSymbol.
func parseDocumentSymbols(raw json.RawMessage) ([]DocumentSymbol, error) {
	var hier []DocumentSymbol
	if err := json.Unmarshal(raw, &hier); err == nil && hier != nil && hasHierarchicalFields(hier) {
		return hier, nil
	}
	var flat []struct {
		Name     string `json:"name"`
		Kind     int    `json:"kind"`
		Location struct {
			Range Range `json:"range"`
		} `json:"location"`
		ContainerName string `json:"containerName,omitempty"`
	}
	if err := json.Unmarshal(raw, &flat); err != nil {
		return nil, fmt.Errorf("lsp: decode documentSymbol: %w", err)
	}
	out := make([]DocumentSymbol, 0, len(flat))
	for _, f := range flat {
		out = append(out, DocumentSymbol{
			Name:           f.Name,
			Kind:           f.Kind,
			Range:          f.Location.Range,
			SelectionRange: f.Location.Range,
		})
	}
	return out, nil
}

func hasHierarchicalFields(items []DocumentSymbol) bool {
	for _, it := range items {
		if it.Name != "" && (it.Range.End.Line > 0 || it.SelectionRange.End.Line > 0 || it.Kind != 0) {
			return true
		}
	}
	return false
}

// parseWorkspaceEdit normalizes both the `changes` and `documentChanges`
// shapes of a WorkspaceEdit into a flat list of TextEdit entries, each
// carrying the source file path and URI.
func parseWorkspaceEdit(raw json.RawMessage) (WorkspaceEdit, error) {
	var top struct {
		Changes         map[string][]TextEdit `json:"changes,omitempty"`
		DocumentChanges []json.RawMessage     `json:"documentChanges,omitempty"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return WorkspaceEdit{}, fmt.Errorf("lsp: decode rename edit: %w", err)
	}
	var out WorkspaceEdit
	for uri, edits := range top.Changes {
		path := pathFromURIString(uri)
		for _, e := range edits {
			e.URI = uri
			e.Path = path
			out.Edits = append(out.Edits, e)
		}
	}
	for _, doc := range top.DocumentChanges {
		var change struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Edits []TextEdit `json:"edits"`
		}
		if err := json.Unmarshal(doc, &change); err != nil {
			continue
		}
		path := pathFromURIString(change.TextDocument.URI)
		for _, e := range change.Edits {
			e.URI = change.TextDocument.URI
			e.Path = path
			out.Edits = append(out.Edits, e)
		}
	}
	sort.SliceStable(out.Edits, func(i, j int) bool {
		if out.Edits[i].Path != out.Edits[j].Path {
			return out.Edits[i].Path < out.Edits[j].Path
		}
		if out.Edits[i].Range.Start.Line != out.Edits[j].Range.Start.Line {
			return out.Edits[i].Range.Start.Line < out.Edits[j].Range.Start.Line
		}
		return out.Edits[i].Range.Start.Character < out.Edits[j].Range.Start.Character
	})
	return out, nil
}

// parseCodeActions normalizes (Command | CodeAction)[] into []CodeAction.
// Commands lack a `kind` and never carry an `edit`; CodeActions have both.
func parseCodeActions(raw json.RawMessage) ([]CodeAction, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("lsp: decode codeAction: %w", err)
	}
	out := make([]CodeAction, 0, len(arr))
	for _, item := range arr {
		var probe struct {
			Title   string          `json:"title"`
			Kind    string          `json:"kind,omitempty"`
			Command json.RawMessage `json:"command,omitempty"`
			Edit    json.RawMessage `json:"edit,omitempty"`
		}
		if err := json.Unmarshal(item, &probe); err != nil {
			continue
		}
		if probe.Title == "" {
			continue
		}
		out = append(out, CodeAction{
			Title:   probe.Title,
			Kind:    probe.Kind,
			HasEdit: len(probe.Edit) > 0 && string(probe.Edit) != "null",
		})
	}
	return out, nil
}

func pathFromURIString(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return uri
	}
	return filepath.FromSlash(u.Path)
}
