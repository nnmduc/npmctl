// Package schemadata carries the vendored Nginx Proxy Manager OpenAPI document.
//
// The schema lives here, embedded in the binary, rather than in testdata/ for two
// reasons: `npmctl schema check` needs it at runtime, and go:embed cannot reach outside
// its own package directory. Keeping one copy means the drift detector and the tests
// compare against exactly the same bytes.
//
// It is the DEREFERENCED document, matching what a live instance serves from
// GET /api/schema.
package schemadata

import (
	"embed"
	"encoding/json"
	"fmt"
)

// Version is the NPM release this schema was taken from. Every payload and behavioural
// contract npmctl relies on was verified against it.
const Version = "2.15.1"

//go:embed schema-2.15.1.json
var files embed.FS

const fileName = "schema-2.15.1.json"

// Bytes returns the raw vendored schema.
func Bytes() ([]byte, error) {
	b, err := files.ReadFile(fileName)
	if err != nil {
		return nil, fmt.Errorf("read embedded schema: %w", err)
	}
	return b, nil
}

// Document returns the vendored schema decoded.
func Document() (map[string]any, error) {
	b, err := Bytes()
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("parse embedded schema: %w", err)
	}
	return doc, nil
}
