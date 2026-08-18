// Traversal and set helpers for the schema comparison.
package schema

import "sort"

// operations indexes every path × method pair in a document.
func operations(doc map[string]any) map[Operation]map[string]any {
	out := map[Operation]map[string]any{}
	paths, _ := doc["paths"].(map[string]any)
	for path, item := range paths {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, method := range httpMethods {
			if op, ok := entry[method].(map[string]any); ok {
				out[Operation{Method: method, Path: path}] = op
			}
		}
	}
	return out
}

// Operations returns the sorted operation list for a document, used by the parity
// checklist as well as the diff.
func Operations(doc map[string]any) []Operation {
	return sortedOps(operations(doc))
}

// IsDeferred reports whether an operation is outside the v1 surface.
func IsDeferred(op Operation) bool { return isDeferred(op) }

func sortedOps(m map[Operation]map[string]any) []Operation {
	out := make([]Operation, 0, len(m))
	for op := range m {
		out = append(out, op)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// requestSchema extracts an operation's JSON request-body schema, or nil.
func requestSchema(op map[string]any) map[string]any {
	rb, ok := op["requestBody"].(map[string]any)
	if !ok {
		return nil
	}
	content, ok := rb["content"].(map[string]any)
	if !ok {
		return nil
	}
	appJSON, ok := content["application/json"].(map[string]any)
	if !ok {
		return nil
	}
	sch, _ := appJSON["schema"].(map[string]any)
	return sch
}

func propertyNames(sch map[string]any) []string {
	props, _ := sch["properties"].(map[string]any)
	out := make([]string, 0, len(props))
	for name := range props {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// stringList normalizes a JSON array of scalars into a sorted string slice.
func stringList(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// diffStrings returns members of a that are absent from b.
func diffStrings(a, b []string) []string {
	set := make(map[string]struct{}, len(b))
	for _, s := range b {
		set[s] = struct{}{}
	}
	var out []string
	for _, s := range a {
		if _, ok := set[s]; !ok {
			out = append(out, s)
		}
	}
	return out
}

// deepCopyMap copies a decoded JSON document so Normalize never mutates its input —
// the caller may still need the original, e.g. to print it.
func deepCopyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = deepCopyValue(v)
	}
	return out
}

func deepCopyValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return deepCopyMap(t)
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = deepCopyValue(item)
		}
		return out
	default:
		return v
	}
}
