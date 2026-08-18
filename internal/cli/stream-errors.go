package cli

import (
	"fmt"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

// streamDomainsOnUpdateError explains an asymmetry in NPM's own schema: POST
// /nginx/streams accepts domain_names, PUT /nginx/streams/{id} does not, and both
// bodies forbid unknown properties.
func streamDomainsOnUpdateError() error {
	return exitcode.New(exitcode.Usage,
		"--domain cannot be used with `stream update`: the update endpoint does not accept "+
			"domain_names and would reject the request. Recreate the stream to change its domains.")
}

// streamRefError explains that streams are addressed numerically.
func streamRefError(ref string) error {
	return exitcode.New(exitcode.Usage,
		"%q is not a stream reference: pass a numeric stream ID or an incoming port number", ref)
}

// portLabel renders a stream's handle for messages.
func portLabel(port int) string { return fmt.Sprintf("port %d", port) }
