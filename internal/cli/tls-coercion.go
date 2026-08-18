// Detection of NPM's silent TLS flag coercion.
//
// internal/host.js `cleanSslHstsData` quietly forces a cascade of flags off when their
// prerequisites are missing: no certificate_id clears ssl_forced AND http2_support; no
// ssl_forced clears hsts_enabled; no hsts_enabled clears hsts_subdomains. The write
// still answers 200 with the coerced object.
//
// That matters because the flags are security-relevant. An operator who runs --hsts and
// sees exit 0 will reasonably believe HSTS is on. The schema cannot express this rule, so
// npmctl compares what it asked for against what came back and says so.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// tlsFlagPrereq maps each coercible flag to the condition that must hold for NPM to
// honour it.
var tlsFlagPrereq = map[string]string{
	"ssl_forced":      "the host has no certificate attached (set --certificate-id)",
	"http2_support":   "HTTP/2 requires TLS, and the host has no certificate attached (set --certificate-id)",
	"hsts_enabled":    "HSTS requires --ssl-forced to be on",
	"hsts_subdomains": "HSTS for subdomains requires --hsts to be on",
}

// warnTLSCoercion reports any flag the caller asked to enable that the server turned
// off. It is advisory: the write succeeded, but not as requested.
func warnTLSCoercion(w io.Writer, requested map[string]any, applied any) {
	if len(requested) == 0 || applied == nil {
		return
	}
	got, ok := asMap(applied)
	if !ok {
		return
	}

	var coerced []string
	for flag := range tlsFlagPrereq {
		want, asked := requested[flag]
		if !asked || want != true {
			continue
		}
		if isTrue(got[flag]) {
			continue
		}
		coerced = append(coerced, flag)
	}
	if len(coerced) == 0 {
		return
	}
	sort.Strings(coerced)
	fmt.Fprintf(w, "warning: NPM did not apply %d requested setting(s):\n", len(coerced))
	for _, flag := range coerced {
		fmt.Fprintf(w, "  %s stayed off — %s\n", flag, tlsFlagPrereq[flag])
	}
}

// asMap re-encodes a typed API object into a generic map so one comparison works for
// every host-like resource.
func asMap(v any) (map[string]any, bool) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, false
	}
	return m, true
}

func isTrue(v any) bool {
	b, ok := v.(bool)
	return ok && b
}
