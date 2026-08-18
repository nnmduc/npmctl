package output

import (
	"encoding/json"
	"io"
)

// renderJSON writes indented JSON. Unexported: reachable only via Render, which
// has already scrubbed the value.
func renderJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
