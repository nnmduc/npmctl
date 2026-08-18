package npmapi

import "fmt"

// sprintf is a local alias so resource files need not each import fmt for a single
// message.
func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// notFoundFor builds the 404 a domain lookup returns, so every resource reports a
// missed name the same way and the CLI maps it to exit 5 uniformly.
func notFoundFor(kind, domain, path string) error {
	return &APIError{
		Status:  404,
		Code:    404,
		Message: fmt.Sprintf("no %s serves %q", kind, domain),
		Method:  "GET",
		Path:    path,
	}
}
