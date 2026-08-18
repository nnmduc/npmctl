// Interactive prompts. Secrets are read without echo and never accepted as a
// flag or argument, so they cannot reach shell history, ps output, or an agent
// transcript of the command it ran.
package cli

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/nnmduc/npmctl/internal/exitcode"
	"golang.org/x/term"
)

func promptLine(rt *runtime, prompt string) (string, error) {
	fmt.Fprint(rt.stderr, prompt)
	line, err := bufio.NewReader(rt.stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return "", exitcode.Wrap(exitcode.Usage, err, "reading input")
	}
	return strings.TrimSpace(line), nil
}

// promptSecret reads without echo. The password is never accepted as a flag or
// argument, so it cannot land in shell history, ps output, or an agent's
// transcript of the command it ran.

// promptSecret reads without echo. The password is never accepted as a flag or
// argument, so it cannot land in shell history, ps output, or an agent's
// transcript of the command it ran.

// promptSecret reads without echo. The password is never accepted as a flag or
// argument, so it cannot land in shell history, ps output, or an agent's
// transcript of the command it ran.
func promptSecret(rt *runtime, prompt string) (string, error) {
	f, ok := rt.stdin.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		// Non-interactive: read one line rather than failing, so `echo | npmctl`
		// and CI both work. NPMCTL_PASSWORD is the documented path.
		return promptLine(rt, prompt)
	}
	fmt.Fprint(rt.stderr, prompt)
	b, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(rt.stderr)
	if err != nil {
		return "", exitcode.Wrap(exitcode.Usage, err, "reading password")
	}
	return strings.TrimSpace(string(b)), nil
}

// isPlaintextURL reports whether a URL uses unencrypted HTTP.
//
// A missing scheme is treated as plaintext: npmctl requires an explicit scheme, and
// guessing "probably https" on a credential path is the wrong default.
func isPlaintextURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return true
	}
	return !strings.EqualFold(u.Scheme, "https")
}
