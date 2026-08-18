package cli

import (
	"strconv"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

// parsePositiveInt converts a positional argument, reporting a usage error rather
// than letting a malformed id become part of a request path.
func parsePositiveInt(s, what string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, exitcode.New(exitcode.Usage, "%s must be a positive integer, got %q", what, s)
	}
	return n, nil
}
