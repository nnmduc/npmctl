// Request-body comparison: property sets, required fields, additionalProperties,
// minProperties and per-property enums. This is the check that would have caught the
// certificate meta and access-list field-name bugs before they reached a live instance.
package schema

import (
	"fmt"
	"strings"
)

// compareBodies diffs the JSON request-body schema of one operation.
func compareBodies(op Operation, vendored, live map[string]any) []Finding {
	vb := requestSchema(vendored)
	lb := requestSchema(live)
	if vb == nil && lb == nil {
		return nil
	}
	deferred := isDeferred(op)
	if vb == nil || lb == nil {
		return []Finding{{
			Operation: op.String(),
			Kind:      "request-body-presence",
			Detail:    fmt.Sprintf("request body present in vendored=%t, live=%t", vb != nil, lb != nil),
			Deferred:  deferred,
		}}
	}

	var out []Finding
	// Property sets.
	vProps := propertyNames(vb)
	lProps := propertyNames(lb)
	for _, name := range diffStrings(vProps, lProps) {
		out = append(out, Finding{
			Operation: op.String(), Kind: "property-removed",
			Detail: fmt.Sprintf("property %q no longer accepted", name), Deferred: deferred,
		})
	}
	for _, name := range diffStrings(lProps, vProps) {
		out = append(out, Finding{
			Operation: op.String(), Kind: "property-added",
			Detail: fmt.Sprintf("property %q is new", name), Deferred: deferred,
		})
	}
	// required
	for _, name := range diffStrings(stringList(vb["required"]), stringList(lb["required"])) {
		out = append(out, Finding{
			Operation: op.String(), Kind: "required-removed",
			Detail: fmt.Sprintf("%q is no longer required", name), Deferred: deferred,
		})
	}
	for _, name := range diffStrings(stringList(lb["required"]), stringList(vb["required"])) {
		out = append(out, Finding{
			Operation: op.String(), Kind: "required-added",
			Detail: fmt.Sprintf("%q is now required", name), Deferred: deferred,
		})
	}
	// additionalProperties: a change here changes whether unknown keys are rejected.
	if fmt.Sprint(vb["additionalProperties"]) != fmt.Sprint(lb["additionalProperties"]) {
		out = append(out, Finding{
			Operation: op.String(), Kind: "additional-properties-changed",
			Detail:   fmt.Sprintf("additionalProperties %v -> %v", vb["additionalProperties"], lb["additionalProperties"]),
			Deferred: deferred,
		})
	}
	// minProperties
	if fmt.Sprint(vb["minProperties"]) != fmt.Sprint(lb["minProperties"]) {
		out = append(out, Finding{
			Operation: op.String(), Kind: "min-properties-changed",
			Detail:   fmt.Sprintf("minProperties %v -> %v", vb["minProperties"], lb["minProperties"]),
			Deferred: deferred,
		})
	}
	// Per-property enums, where a narrowing silently breaks valid input.
	out = append(out, compareEnums(op, vb, lb, deferred)...)
	return out
}

func compareEnums(op Operation, vb, lb map[string]any, deferred bool) []Finding {
	var out []Finding
	vp, _ := vb["properties"].(map[string]any)
	lp, _ := lb["properties"].(map[string]any)
	for name := range vp {
		vSpec, ok1 := vp[name].(map[string]any)
		lSpec, ok2 := lp[name].(map[string]any)
		if !ok1 || !ok2 {
			continue
		}
		vEnum := stringList(vSpec["enum"])
		lEnum := stringList(lSpec["enum"])
		if len(vEnum) == 0 && len(lEnum) == 0 {
			continue
		}
		if strings.Join(vEnum, ",") != strings.Join(lEnum, ",") {
			out = append(out, Finding{
				Operation: op.String(), Kind: "enum-changed",
				Detail:   fmt.Sprintf("property %q enum [%s] -> [%s]", name, strings.Join(vEnum, ","), strings.Join(lEnum, ",")),
				Deferred: deferred,
			})
		}
	}
	return out
}
