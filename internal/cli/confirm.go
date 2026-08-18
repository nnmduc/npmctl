package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/nnmduc/npmctl/internal/exitcode"
	"github.com/nnmduc/npmctl/internal/output"
)

// confirmInteractively demands a typed confirmation at a real terminal.
//
// It refuses outright when stdout is not a TTY rather than falling back to a
// flag. That is the point: --yes and NPMCTL_ALLOW_WRITE are both things an
// automated caller can set for itself, so the Privileged tier needs a factor
// that cannot be forged by a process with no human attached.
// interactiveCheck reports whether a real terminal is attached. It is a variable
// so tests can exercise the confirmation prompt itself — including the typed-match
// logic — rather than only its refusal path. Production always uses output.IsTTY.
var interactiveCheck = output.IsTTY

func (g *gate) confirmInteractively(op Op) error {
	rt := g.rt
	if !interactiveCheck(rt.stdout) {
		return exitcode.New(exitcode.Refused,
			"refusing to %s %s: this is a %s operation and requires an interactive terminal",
			op.Verb, op.Resource, op.Tier)
	}
	want := op.Kind
	if op.Verb == "delete" {
		want = "delete " + op.Kind
	}
	fmt.Fprintf(rt.stderr, "\n%s operation: %s %s\n", strings.ToUpper(op.Tier.String()), op.Verb, op.Resource)
	if op.Note != "" {
		fmt.Fprintf(rt.stderr, "WARNING: %s\n", op.Note)
	}
	fmt.Fprintf(rt.stderr, "Type %q to continue: ", want)

	line, err := bufio.NewReader(rt.stdin).ReadString('\n')
	if err != nil {
		return exitcode.Wrap(exitcode.Refused, err, "reading confirmation")
	}
	if strings.TrimSpace(line) != want {
		return exitcode.New(exitcode.Refused, "confirmation did not match — aborted")
	}
	return nil
}

// diffLines renders a line-level diff of an advanced_config change, so a raw
// nginx block is reviewed rather than pasted blind.
func diffLines(before, after string) string {
	var b strings.Builder
	oldLines := strings.Split(before, "\n")
	newLines := strings.Split(after, "\n")
	max := len(oldLines)
	if len(newLines) > max {
		max = len(newLines)
	}
	for i := 0; i < max; i++ {
		var o, n string
		if i < len(oldLines) {
			o = oldLines[i]
		}
		if i < len(newLines) {
			n = newLines[i]
		}
		switch {
		case o == n && o != "":
			fmt.Fprintf(&b, "  %s\n", o)
		case o != "" && n == "":
			fmt.Fprintf(&b, "- %s\n", o)
		case o == "" && n != "":
			fmt.Fprintf(&b, "+ %s\n", n)
		case o != n:
			fmt.Fprintf(&b, "- %s\n+ %s\n", o, n)
		}
	}
	return b.String()
}
