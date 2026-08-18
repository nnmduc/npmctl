package npmapi

import (
	"sort"
	"strings"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

// Payload builds a partial-update body under the two constraints every NPM
// write schema imposes: additionalProperties:false and, on PUT, minProperties:1.
//
// It is a map rather than a struct because PUT must send ONLY the fields the
// caller actually changed — a struct with omitempty cannot distinguish "set
// false" from "unset", and would silently disable a host whose enabled flag the
// caller never mentioned. The allowlist turns "never emit an unknown key" from a
// review checklist item into a compile-time-shaped runtime guarantee.
type Payload struct {
	allowed map[string]struct{}
	fields  map[string]any
	name    string
}

// NewPayload returns a builder restricted to the given schema properties.
func NewPayload(name string, allowed []string) *Payload {
	set := make(map[string]struct{}, len(allowed))
	for _, a := range allowed {
		set[a] = struct{}{}
	}
	return &Payload{allowed: set, fields: map[string]any{}, name: name}
}

// Set records a field. An unknown key is a programming error surfaced here
// rather than as an opaque "should NOT have additional properties" 400.
func (p *Payload) Set(key string, val any) *Payload {
	if _, ok := p.allowed[key]; !ok {
		panic("npmapi: field " + key + " is not permitted on " + p.name)
	}
	p.fields[key] = val
	return p
}

// SetIf records a field only when set is true, which is how a CLI flag that was
// never provided stays absent from the body.
func (p *Payload) SetIf(set bool, key string, val any) *Payload {
	if set {
		p.Set(key, val)
	}
	return p
}

// Len reports how many fields are set.
func (p *Payload) Len() int { return len(p.fields) }

// Keys lists the set fields, sorted, for previews and diffs.
func (p *Payload) Keys() []string {
	keys := make([]string, 0, len(p.fields))
	for k := range p.fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Map returns the body. It refuses to produce an empty object because the
// schema's minProperties:1 would reject it at the server with a message that
// does not explain the caller passed no flags.
func (p *Payload) Map() (map[string]any, error) {
	if len(p.fields) == 0 {
		return nil, exitcode.New(exitcode.Usage,
			"no fields to update: pass at least one field flag (%s)", strings.Join(p.allowedKeys(), ", "))
	}
	return p.fields, nil
}

func (p *Payload) allowedKeys() []string {
	keys := make([]string, 0, len(p.allowed))
	for k := range p.allowed {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
