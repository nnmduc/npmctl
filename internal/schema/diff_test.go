package schema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nnmduc/npmctl/internal/schemadata"
)

func vendored(t *testing.T) map[string]any {
	t.Helper()
	doc, err := schemadata.Document()
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func fixture(t *testing.T, name string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// TestNoDriftAgainstItself is the baseline: a schema must not drift from itself.
func TestNoDriftAgainstItself(t *testing.T) {
	v := vendored(t)
	report := Compare(v, vendored(t))
	if len(report.Findings) != 0 {
		t.Fatalf("a schema drifted from itself: %+v", report.Findings)
	}
	if report.VendoredOperations != 68 || report.LiveOperations != 68 {
		t.Errorf("operation counts = %d/%d, want 68/68", report.VendoredOperations, report.LiveOperations)
	}
}

// TestVolatileFieldsDoNotCauseDrift is R15. NPM rewrites info.version from its build and
// servers[0].url from the request's Origin header, so without normalization every single
// check would report drift and the detector would be worthless.
func TestVolatileFieldsDoNotCauseDrift(t *testing.T) {
	report := Compare(vendored(t), fixture(t, "schema-volatile-only.json"))
	if len(report.Findings) != 0 {
		t.Fatalf("info.version / servers[0].url caused false drift: %+v", report.Findings)
	}
	if report.Breaking() {
		t.Error("volatile-only differences must not be breaking")
	}
}

// TestNormalizeDoesNotMutateInput matters because the caller may still print the document.
func TestNormalizeDoesNotMutateInput(t *testing.T) {
	v := vendored(t)
	original := v["info"].(map[string]any)["version"]
	_ = Normalize(v)
	if got := v["info"].(map[string]any)["version"]; got != original {
		t.Errorf("Normalize mutated its input: info.version is now %v", got)
	}
}

// TestDetectsRequestBodyDrift is the check that would have caught the certificate meta
// and access-list field-name bugs before they reached a live instance.
func TestDetectsRequestBodyDrift(t *testing.T) {
	report := Compare(vendored(t), fixture(t, "schema-drifted.json"))
	if !report.Breaking() {
		t.Fatal("drift in an implemented path must be breaking")
	}

	kinds := map[string]string{}
	for _, f := range report.Findings {
		kinds[f.Kind] = f.Detail
	}
	for _, want := range []string{"property-removed", "property-added", "additional-properties-changed"} {
		if _, ok := kinds[want]; !ok {
			t.Errorf("expected a %q finding; got %+v", want, report.Findings)
		}
	}
	// The report has to name the field, or it is not actionable.
	if detail := kinds["property-removed"]; !strings.Contains(detail, "block_exploits") {
		t.Errorf("property-removed should name block_exploits, got %q", detail)
	}
}

// TestDeferredPathDriftIsInformational: v1 does not implement /users, so drift there
// cannot break this binary and must not fail the check.
func TestDeferredPathDriftIsInformational(t *testing.T) {
	report := Compare(vendored(t), fixture(t, "schema-drifted-deferred.json"))
	if len(report.Findings) == 0 {
		t.Fatal("drift in a deferred path should still be reported")
	}
	for _, f := range report.Findings {
		if !f.Deferred {
			t.Errorf("finding on a deferred path is marked breaking: %+v", f)
		}
	}
	if report.Breaking() {
		t.Error("drift confined to deferred paths must not fail the check")
	}
}

// TestDetectsRemovedOperation covers an upgrade that drops an endpoint entirely.
func TestDetectsRemovedOperation(t *testing.T) {
	live := vendored(t)
	paths := live["paths"].(map[string]any)
	delete(paths, "/nginx/proxy-hosts/{hostID}")

	report := Compare(vendored(t), live)
	if !report.Breaking() {
		t.Fatal("removing an implemented operation must be breaking")
	}
	var found bool
	for _, f := range report.Findings {
		if f.Kind == "operation-removed" && strings.Contains(f.Operation, "/nginx/proxy-hosts/{hostID}") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an operation-removed finding: %+v", report.Findings)
	}
}

// TestDetectsRequiredChange: a newly required field breaks previously valid requests.
func TestDetectsRequiredChange(t *testing.T) {
	live := vendored(t)
	sch := live["paths"].(map[string]any)["/nginx/proxy-hosts/{hostID}"].(map[string]any)["put"].(map[string]any)["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	sch["required"] = []any{"forward_host"}

	report := Compare(vendored(t), live)
	var found bool
	for _, f := range report.Findings {
		if f.Kind == "required-added" && strings.Contains(f.Detail, "forward_host") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a required-added finding: %+v", report.Findings)
	}
}

// TestDetectsEnumNarrowing: dropping an enum member silently rejects input that used
// to be valid.
func TestDetectsEnumNarrowing(t *testing.T) {
	live := vendored(t)
	sch := live["paths"].(map[string]any)["/nginx/proxy-hosts/{hostID}"].(map[string]any)["put"].(map[string]any)["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	props := sch["properties"].(map[string]any)
	scheme := props["forward_scheme"].(map[string]any)
	scheme["enum"] = []any{"https"} // http dropped

	report := Compare(vendored(t), live)
	var found bool
	for _, f := range report.Findings {
		if f.Kind == "enum-changed" && strings.Contains(f.Detail, "forward_scheme") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an enum-changed finding: %+v", report.Findings)
	}
}

// TestParityChecklistCoversEveryOperation enumerates path × method, not paths.
//
// A path-only checklist passes while a method is missing — which is exactly how GET /
// went unmapped in the original endpoint map. Every operation must be classified as
// implemented or deferred-with-reason; none may be unaccounted for.
func TestParityChecklistCoversEveryOperation(t *testing.T) {
	ops := Operations(vendored(t))
	if len(ops) != 68 {
		t.Fatalf("vendored schema has %d operations, want 68", len(ops))
	}

	implemented, deferred := 0, 0
	for _, op := range ops {
		if IsDeferred(op) {
			deferred++
			continue
		}
		implemented++
	}
	// 13 /users operations plus PUT /settings/{settingID}.
	if deferred != 14 {
		t.Errorf("deferred operations = %d, want 14 (the /users family plus PUT /settings/{settingID})", deferred)
	}
	if implemented+deferred != len(ops) {
		t.Errorf("accounting gap: %d implemented + %d deferred != %d operations", implemented, deferred, len(ops))
	}
	if implemented != 54 {
		t.Errorf("implemented operations = %d, want 54", implemented)
	}
}

// TestHealthOperationIsInTheV1Surface guards the specific omission that motivated
// counting operations rather than paths.
func TestHealthOperationIsInTheV1Surface(t *testing.T) {
	for _, op := range Operations(vendored(t)) {
		if op.Path == "/" && op.Method == "get" {
			if IsDeferred(op) {
				t.Error("GET / (health) must be part of the v1 surface")
			}
			return
		}
	}
	t.Error("GET / (health) is missing from the vendored schema")
}

// TestDeferredClassification pins exactly which operations are out of scope.
func TestDeferredClassification(t *testing.T) {
	cases := []struct {
		op   Operation
		want bool
	}{
		{Operation{Method: "get", Path: "/users"}, true},
		{Operation{Method: "put", Path: "/users/{userID}/auth"}, true},
		{Operation{Method: "post", Path: "/users/{userID}/login"}, true},
		{Operation{Method: "put", Path: "/settings/{settingID}"}, true},
		{Operation{Method: "get", Path: "/settings/{settingID}"}, false},
		{Operation{Method: "get", Path: "/settings"}, false},
		{Operation{Method: "get", Path: "/"}, false},
		{Operation{Method: "post", Path: "/nginx/certificates"}, false},
	}
	for _, tc := range cases {
		if got := IsDeferred(tc.op); got != tc.want {
			t.Errorf("IsDeferred(%s) = %v, want %v", tc.op, got, tc.want)
		}
	}
}
