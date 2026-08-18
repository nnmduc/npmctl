// Package npmapi is a hand-written client for the Nginx Proxy Manager v2.15.1
// REST API. No codegen: the schema is vendored as a test fixture instead, so
// drift is detected by `npmctl schema check` rather than hidden behind a
// regenerated client.
package npmapi

// Timestamps are present on every persisted NPM object. ModifiedOn is the
// compare-and-swap token the write gate uses: NPM offers no ETag or If-Match,
// so this field is the only way to notice someone else changed the row between
// preview and write.
type Timestamps struct {
	CreatedOn  string `json:"created_on,omitempty"`
	ModifiedOn string `json:"modified_on,omitempty"`
}

// Owner is the embedded user object returned under ?expand=owner.
type Owner struct {
	ID    int    `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// Meta carries per-resource extras. Only the nginx health fields are read here:
// NPM answers 200 on a write whose nginx reload failed, recording the failure in
// meta rather than the status code.
type Meta map[string]any

// NginxOnline reports the reload state recorded in meta. The second return
// value is false when the field is absent, which is not the same as unhealthy —
// a caller must not treat "unknown" as "broken".
func (m Meta) NginxOnline() (online bool, present bool) {
	v, ok := m["nginx_online"]
	if !ok || v == nil {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

// NginxErr returns the verbatim nginx error text NPM recorded, if any.
func (m Meta) NginxErr() string {
	v, ok := m["nginx_err"]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}
