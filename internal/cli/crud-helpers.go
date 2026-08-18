package cli

import (
	"sort"
	"strings"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

// firstDomain returns a record's primary domain for labels.
func firstDomain(domains []string) string {
	if len(domains) > 0 {
		return domains[0]
	}
	return ""
}

// requireFlags reports the missing required flags by name, so the error names the
// flag to add rather than surfacing a generic 400 from the API.
func requireFlags(present map[string]bool) error {
	var missing []string
	for name, ok := range present {
		if !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return exitcode.New(exitcode.Usage, "missing required flag(s): %s", strings.Join(missing, ", "))
}
