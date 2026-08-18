package output

import (
	"io"
	"os"

	"golang.org/x/term"
)

// IsTTY reports whether w is an interactive terminal. Used for two distinct
// decisions: default output format, and whether a Privileged-tier confirmation
// can be obtained at all.
func IsTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// Resolve picks the effective format: an explicit -o always wins, otherwise a
// terminal gets a table and a pipe gets JSON.
func Resolve(override Format, w io.Writer) Format {
	if override != "" {
		return override
	}
	if IsTTY(w) {
		return FormatTable
	}
	return FormatJSON
}
