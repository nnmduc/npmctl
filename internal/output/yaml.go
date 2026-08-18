package output

import (
	"io"

	"gopkg.in/yaml.v3"
)

// renderYAML writes YAML. Unexported: reachable only via Render.
func renderYAML(w io.Writer, v any) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return err
	}
	return enc.Close()
}
