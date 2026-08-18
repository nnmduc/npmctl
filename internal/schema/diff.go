// Package schema compares a live NPM OpenAPI document against the vendored copy.
//
// This is the drift detector that partially compensates for a fixtures-only test
// strategy. It has a hard limit worth stating plainly: it cannot see BEHAVIOURAL
// drift. Partial-update semantics, the TLS-flag coercion cascade, revoke-on-delete,
// and the comma-separated `expand` encoding are all implementation contracts that no
// schema expresses. Those are covered by the lab instance and the opt-in E2E tests.
package schema

import (
	"strings"
)

// httpMethods are the operation keys worth comparing.
var httpMethods = []string{"get", "post", "put", "delete", "patch"}

// Operation identifies one path × method pair. Counting operations rather than paths
// is deliberate: a path-only checklist passes while a method is missing, which is
// exactly how GET / went unmapped in the original endpoint map.

// Operation identifies one path × method pair. Counting operations rather than paths
// is deliberate: a path-only checklist passes while a method is missing, which is
// exactly how GET / went unmapped in the original endpoint map.
type Operation struct {
	Method string
	Path   string
}

func (o Operation) String() string { return strings.ToUpper(o.Method) + " " + o.Path }

// Finding is one difference between two schemas.

// Finding is one difference between two schemas.
type Finding struct {
	Operation string `json:"operation"`
	Kind      string `json:"kind"`
	Detail    string `json:"detail"`
	// Deferred marks a finding in a path v1 does not implement. Those are
	// informational: drift in an unimplemented path cannot break this binary.
	Deferred bool `json:"deferred"`
}

// Report is the outcome of a comparison.

// Report is the outcome of a comparison.
type Report struct {
	Findings []Finding `json:"findings"`
	// Operations counted on each side, for a quick sanity check.
	VendoredOperations int `json:"vendored_operations"`
	LiveOperations     int `json:"live_operations"`
}

// Breaking reports whether any finding affects an implemented path.

// Breaking reports whether any finding affects an implemented path.
func (r *Report) Breaking() bool {
	for _, f := range r.Findings {
		if !f.Deferred {
			return true
		}
	}
	return false
}

// deferredPrefixes are the paths v1 deliberately does not implement.

// deferredPrefixes are the paths v1 deliberately does not implement.
var deferredPrefixes = []string{"/users"}

// isDeferred reports whether an operation is outside the v1 surface.

// isDeferred reports whether an operation is outside the v1 surface.
func isDeferred(op Operation) bool {
	for _, p := range deferredPrefixes {
		if strings.HasPrefix(op.Path, p) {
			return true
		}
	}
	// PUT /settings/{settingID} is deferred; the GETs are implemented.
	if op.Path == "/settings/{settingID}" && op.Method == "put" {
		return true
	}
	return false
}

// Normalize strips the fields NPM rewrites per request.
//
// The server derives info.version from its build and servers[0].url from the request's
// Origin header, so a naive diff reports drift on every single call. Removing them is
// what makes a "no drift" result meaningful.

// Normalize strips the fields NPM rewrites per request.
//
// The server derives info.version from its build and servers[0].url from the request's
// Origin header, so a naive diff reports drift on every single call. Removing them is
// what makes a "no drift" result meaningful.
func Normalize(doc map[string]any) map[string]any {
	out := deepCopyMap(doc)
	if info, ok := out["info"].(map[string]any); ok {
		delete(info, "version")
	}
	if servers, ok := out["servers"].([]any); ok && len(servers) > 0 {
		if first, ok := servers[0].(map[string]any); ok {
			delete(first, "url")
		}
	}
	return out
}

// Compare diffs two dereferenced OpenAPI documents.

// Compare diffs two dereferenced OpenAPI documents.
func Compare(vendored, live map[string]any) *Report {
	v := Normalize(vendored)
	l := Normalize(live)

	vOps := operations(v)
	lOps := operations(l)
	report := &Report{VendoredOperations: len(vOps), LiveOperations: len(lOps)}

	// Missing and added operations.
	for _, op := range sortedOps(vOps) {
		if _, ok := lOps[op]; !ok {
			report.Findings = append(report.Findings, Finding{
				Operation: op.String(),
				Kind:      "operation-removed",
				Detail:    "present in the vendored schema, absent from the live instance",
				Deferred:  isDeferred(op),
			})
		}
	}
	for _, op := range sortedOps(lOps) {
		if _, ok := vOps[op]; !ok {
			report.Findings = append(report.Findings, Finding{
				Operation: op.String(),
				Kind:      "operation-added",
				Detail:    "present on the live instance, absent from the vendored schema",
				Deferred:  isDeferred(op),
			})
		}
	}

	// Request-body shape, for operations both sides have. This is the check that would
	// have caught the certificate `meta` and access-list field-name bugs.
	for _, op := range sortedOps(vOps) {
		lo, ok := lOps[op]
		if !ok {
			continue
		}
		report.Findings = append(report.Findings, compareBodies(op, vOps[op], lo)...)
	}
	return report
}

// compareBodies diffs the JSON request-body schema of one operation.
