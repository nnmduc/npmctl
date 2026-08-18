// Package output renders values for human and machine consumption.
//
// Every renderer in this package funnels through Render, which applies Scrub
// first. Redaction lives here — in the serializer — rather than at call sites,
// because per-call-site redaction has already failed once: it covered the login
// password and missed `cert create --dns-provider`, `audit-log`, and
// `cert download`. Centralising it makes the *next* command safe by default.
package output

import "strings"

// Placeholder replaces any value judged sensitive.
const Placeholder = "[redacted]"

// deniedKeys are matched case-insensitively against object keys. Matching is
// exact, not substring: a substring rule would redact `certificate_id` because
// it contains no secret but shares a prefix with `certificate_key`, and would
// leave genuinely distinct keys uncovered.
var deniedKeys = map[string]struct{}{
	"secret":                   {},
	"password":                 {},
	"token":                    {},
	"challenge_token":          {},
	"dns_provider_credentials": {},
	"certificate_key":          {},
	"authorization":            {},
}

// IsDeniedKey reports whether a key must never have its value rendered.
func IsDeniedKey(k string) bool {
	_, ok := deniedKeys[strings.ToLower(strings.TrimSpace(k))]
	return ok
}

// pemMarkers catch private key material that arrives under a key the denylist
// does not know about — an uploaded PEM, a multipart field, a server error that
// echoes a request body. Defence in depth: the key rules above are the primary
// control, this catches material that slips past them.
var pemMarkers = []string{
	"-----BEGIN PRIVATE KEY",
	"-----BEGIN RSA PRIVATE KEY",
	"-----BEGIN EC PRIVATE KEY",
	"-----BEGIN OPENSSH PRIVATE KEY",
	"-----BEGIN DSA PRIVATE KEY",
	"-----BEGIN ENCRYPTED PRIVATE KEY",
}

// looksSecret reports whether a string value is sensitive on its own merits,
// independent of the key it appeared under.
func looksSecret(s string) bool {
	for _, m := range pemMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	// `Authorization: Bearer <jwt>` rendered as a bare header line.
	if len(s) > 12 && strings.HasPrefix(strings.ToLower(strings.TrimSpace(s)), "bearer ") {
		return true
	}
	return false
}

// Scrub returns a deep copy of v with every sensitive value replaced by
// Placeholder. The input is never mutated: callers hand Scrub live API objects
// and the undo journal must still be able to serialise them unmodified.
func Scrub(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if IsDeniedKey(k) {
				out[k] = Placeholder
				continue
			}
			out[k] = Scrub(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = Scrub(val)
		}
		return out
	case map[string][]string: // http.Header
		out := make(map[string][]string, len(t))
		for k, vals := range t {
			if IsDeniedKey(k) {
				out[k] = []string{Placeholder}
				continue
			}
			cp := make([]string, len(vals))
			for i, s := range vals {
				cp[i] = scrubString(s)
			}
			out[k] = cp
		}
		return out
	case string:
		return scrubString(t)
	default:
		return v
	}
}

func scrubString(s string) string {
	if looksSecret(s) {
		return Placeholder
	}
	return s
}
