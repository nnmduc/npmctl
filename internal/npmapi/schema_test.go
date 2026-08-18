package npmapi

import (
	"sync"
	"testing"

	"github.com/nnmduc/npmctl/internal/schemadata"
)

var (
	schemaOnce sync.Once
	schemaDoc  map[string]any
)

// loadSchema reads the vendored, dereferenced NPM 2.15.1 schema. Tests assert
// against this rather than against prose: the plan's own history is that two
// request bodies transcribed from prose would have failed on first contact.
func loadSchema(t *testing.T) map[string]any {
	t.Helper()
	schemaOnce.Do(func() {
		doc, err := schemadata.Document()
		if err != nil {
			t.Fatalf("load vendored schema: %v", err)
		}
		schemaDoc = doc
	})
	return schemaDoc
}

// schemaRequestProperties returns the JSON request-body properties for one
// operation.
func schemaRequestProperties(t *testing.T, path, method string) map[string]any {
	t.Helper()
	doc := loadSchema(t)
	paths, _ := doc["paths"].(map[string]any)
	p, ok := paths[path].(map[string]any)
	if !ok {
		t.Fatalf("schema has no path %s", path)
	}
	op, ok := p[method].(map[string]any)
	if !ok {
		t.Fatalf("schema has no %s %s", method, path)
	}
	rb, ok := op["requestBody"].(map[string]any)
	if !ok {
		t.Fatalf("%s %s has no requestBody", method, path)
	}
	content := rb["content"].(map[string]any)
	appJSON, ok := content["application/json"].(map[string]any)
	if !ok {
		t.Fatalf("%s %s has no application/json body", method, path)
	}
	sch := appJSON["schema"].(map[string]any)
	props, _ := sch["properties"].(map[string]any)
	return props
}

// TestVendoredSchemaShape pins the surface the whole plan is scoped against. If
// these numbers move, the vendored fixture was replaced and every parity claim
// needs re-checking.
func TestVendoredSchemaShape(t *testing.T) {
	doc := loadSchema(t)
	paths, _ := doc["paths"].(map[string]any)
	if len(paths) != 44 {
		t.Errorf("vendored schema has %d paths, want 44", len(paths))
	}
	ops := 0
	for _, v := range paths {
		item := v.(map[string]any)
		for method := range item {
			switch method {
			case "get", "post", "put", "delete", "patch":
				ops++
			}
		}
	}
	if ops != 68 {
		t.Errorf("vendored schema has %d operations, want 68", ops)
	}
}

// TestHealthOperationExists guards the specific omission a path-only checklist
// hid: GET / has an operationId and is a real operation.
func TestHealthOperationExists(t *testing.T) {
	doc := loadSchema(t)
	paths := doc["paths"].(map[string]any)
	root, ok := paths["/"].(map[string]any)
	if !ok {
		t.Fatal(`schema has no "/" path`)
	}
	get, ok := root["get"].(map[string]any)
	if !ok {
		t.Fatal("GET / is missing")
	}
	if get["operationId"] != "health" {
		t.Errorf(`GET / operationId = %v, want "health"`, get["operationId"])
	}
}

// TestPutIsConstrainedPartialUpdate documents why the payload builder exists.
func TestPutIsConstrainedPartialUpdate(t *testing.T) {
	doc := loadSchema(t)
	paths := doc["paths"].(map[string]any)
	op := paths["/nginx/proxy-hosts/{hostID}"].(map[string]any)["put"].(map[string]any)
	sch := op["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)

	if sch["additionalProperties"] != false {
		t.Error("PUT body should forbid additional properties")
	}
	if sch["minProperties"] != float64(1) {
		t.Errorf("PUT body minProperties = %v, want 1", sch["minProperties"])
	}
	if _, hasRequired := sch["required"]; hasRequired {
		t.Error("PUT should have no required fields — it is a partial update")
	}
}
