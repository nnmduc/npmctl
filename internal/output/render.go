package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Format is an output encoding.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatYAML  Format = "yaml"
)

// ParseFormat validates a -o value.
func ParseFormat(s string) (Format, error) {
	switch Format(strings.ToLower(strings.TrimSpace(s))) {
	case FormatTable:
		return FormatTable, nil
	case FormatJSON:
		return FormatJSON, nil
	case FormatYAML:
		return FormatYAML, nil
	case "":
		return "", nil
	}
	return "", fmt.Errorf("unknown output format %q: want table, json or yaml", s)
}

// normalize converts any Go value into the generic map/slice/scalar shape that
// Scrub understands. Without this step a typed struct would walk straight past
// the denylist — Scrub cannot see the field names of a struct it does not know,
// so every value is funnelled through JSON before redaction.
func normalize(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("serialize: %w", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("normalize: %w", err)
	}
	return out, nil
}

// Render is the ONLY way to write a value to a stream. It normalizes, scrubs,
// then encodes. No renderer is exported on its own, so a future command cannot
// accidentally reach an unscrubbed encoder.
func Render(w io.Writer, f Format, v any) error {
	n, err := normalize(v)
	if err != nil {
		return err
	}
	safe := Scrub(n)
	switch f {
	case FormatJSON:
		return renderJSON(w, safe)
	case FormatYAML:
		return renderYAML(w, safe)
	case FormatTable, "":
		return renderTable(w, safe)
	}
	return fmt.Errorf("unknown output format %q", f)
}

// RenderString writes an already-formatted human message, scrubbed. Used for
// stderr banners and progress lines so they cannot leak either.
func RenderString(w io.Writer, s string) error {
	_, err := fmt.Fprintln(w, scrubString(s))
	return err
}
