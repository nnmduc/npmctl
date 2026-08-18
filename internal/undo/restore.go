package undo

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Spec says how a pre-image of one resource kind becomes a write body.
//
// Two key sets are needed, not one. A pre-image is a full GET response, so it
// carries fields that are legitimately unwritable (id, created_on, the expanded
// owner object). Those must be dropped silently. A key in NEITHER set is
// different: it means the pre-image was written by a version whose schema no
// longer matches, and silently dropping it would produce a partial restore that
// reports success while leaving state wrong.
type Spec struct {
	Kind     string
	Writable []string
	ReadOnly []string
}

// specs is the registry of restorable kinds. A kind absent from here cannot be
// restored, which is a deliberate refusal rather than a best-effort guess.
var specs = map[string]*Spec{}

// Register adds a restorable kind.
func Register(s *Spec) { specs[s.Kind] = s }

// SpecFor returns the spec for a kind.
func SpecFor(kind string) (*Spec, bool) {
	s, ok := specs[kind]
	return s, ok
}

// UnknownKeyError reports a pre-image field the current schema does not accept.
type UnknownKeyError struct {
	Kind string
	Keys []string
}

func (e *UnknownKeyError) Error() string {
	return fmt.Sprintf("pre-image for %s carries field(s) the current API no longer accepts: %s — "+
		"restoring would silently drop them, so this entry cannot be replayed automatically",
		e.Kind, strings.Join(e.Keys, ", "))
}

// Body reconstructs the write body that would restore the captured state.
//
// Only writable fields are emitted, satisfying additionalProperties:false, and
// the result is non-empty so it also satisfies minProperties:1.
func (e *Entry) Body() (map[string]any, error) {
	spec, ok := SpecFor(e.Kind)
	if !ok {
		return nil, fmt.Errorf("no restore rules for kind %q", e.Kind)
	}
	var obj map[string]any
	if err := json.Unmarshal(e.PreImage, &obj); err != nil {
		return nil, fmt.Errorf("parse pre-image: %w", err)
	}

	writable := toSet(spec.Writable)
	readonly := toSet(spec.ReadOnly)

	body := map[string]any{}
	var unknown []string
	for k, v := range obj {
		switch {
		case contains(writable, k):
			body[k] = v
		case contains(readonly, k):
			// Expected response-only field; not part of a write.
		default:
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, &UnknownKeyError{Kind: e.Kind, Keys: unknown}
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("pre-image for %s contains no restorable fields", e.Kind)
	}
	return body, nil
}

// Describe renders a one-line summary for `undo list`.
func (e *Entry) Describe() string {
	return fmt.Sprintf("%s  %-8s %s", e.Time, e.Verb, e.Resource)
}

func toSet(list []string) map[string]struct{} {
	m := make(map[string]struct{}, len(list))
	for _, s := range list {
		m[s] = struct{}{}
	}
	return m
}

func contains(m map[string]struct{}, k string) bool {
	_, ok := m[k]
	return ok
}
