package output

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// Column is a table column bound to a dotted path into the rendered object,
// e.g. {Header: "ONLINE", Key: "meta.nginx_online"}.
type Column struct {
	Header string
	Key    string
}

// RenderWith renders v, using cols when the format is a table. JSON and YAML
// ignore cols and emit the full object — a machine consumer wants everything,
// a human wants the handful of fields that matter.
func RenderWith(w io.Writer, f Format, cols []Column, v any) error {
	if f == FormatTable || f == "" {
		n, err := normalize(v)
		if err != nil {
			return err
		}
		return renderColumns(w, cols, Scrub(n))
	}
	return Render(w, f, v)
}

// lookup walks a dotted path. A missing or null leaf renders as "-" so a table
// row never silently shifts columns.
func lookup(v any, path string) string {
	cur := v
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return "-"
		}
		cur, ok = m[part]
		if !ok {
			return "-"
		}
	}
	return cell(cur)
}

func cell(v any) string {
	switch t := v.(type) {
	case nil:
		return "-"
	case bool:
		if t {
			return "yes"
		}
		return "no"
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	case string:
		if t == "" {
			return "-"
		}
		return strings.ReplaceAll(t, "\n", " ")
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			parts = append(parts, cell(e))
		}
		if len(parts) == 0 {
			return "-"
		}
		return strings.Join(parts, ",")
	default:
		return fmt.Sprintf("%v", t)
	}
}

func renderColumns(w io.Writer, cols []Column, v any) error {
	rows, isList := v.([]any)
	if !isList {
		rows = []any{v}
	}
	if len(cols) == 0 {
		return renderTable(w, v)
	}
	tw := tabwriter.NewWriter(w, 0, 4, 3, ' ', 0)
	heads := make([]string, len(cols))
	for i, c := range cols {
		heads[i] = c.Header
	}
	fmt.Fprintln(tw, strings.Join(heads, "\t"))
	for _, r := range rows {
		vals := make([]string, len(cols))
		for i, c := range cols {
			vals[i] = lookup(r, c.Key)
		}
		fmt.Fprintln(tw, strings.Join(vals, "\t"))
	}
	if len(rows) == 0 {
		fmt.Fprintln(tw, "(none)")
	}
	return tw.Flush()
}

// renderTable is the column-less fallback: a single object becomes KEY/VALUE
// rows, a list becomes columns derived from the union of its scalar fields.
func renderTable(w io.Writer, v any) error {
	switch t := v.(type) {
	case map[string]any:
		tw := tabwriter.NewWriter(w, 0, 4, 3, ' ', 0)
		fmt.Fprintln(tw, "FIELD\tVALUE")
		for _, k := range sortedKeys(t) {
			fmt.Fprintf(tw, "%s\t%s\n", k, cell(t[k]))
		}
		return tw.Flush()
	case []any:
		return renderColumns(w, derivedColumns(t), t)
	default:
		_, err := fmt.Fprintln(w, cell(v))
		return err
	}
}

// derivedColumns picks scalar fields shared by list elements. `id` leads when
// present because it is the handle every other command takes.
func derivedColumns(rows []any) []Column {
	seen := map[string]bool{}
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		for k, val := range m {
			switch val.(type) {
			case map[string]any, []any:
				continue
			}
			seen[k] = true
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		if k != "id" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if seen["id"] {
		keys = append([]string{"id"}, keys...)
	}
	cols := make([]Column, 0, len(keys))
	for _, k := range keys {
		cols = append(cols, Column{Header: strings.ToUpper(k), Key: k})
	}
	return cols
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
