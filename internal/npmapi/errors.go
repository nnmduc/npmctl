package npmapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

// APIError is an error NPM reported in its own envelope:
// {"error": {"code": 400, "message": "..."}}.
type APIError struct {
	Status  int    `json:"-"`
	Code    int    `json:"code"`
	Message string `json:"message"`
	Method  string `json:"-"`
	Path    string `json:"-"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s %s: %s (HTTP %d)", e.Method, e.Path, e.Message, e.Status)
}

// ExitCode maps an API failure onto the stable contract. 401/403 are auth
// failures rather than generic API errors so an agent can tell "your credential
// is bad" from "your request is bad".
func (e *APIError) ExitCode() int {
	switch e.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return exitcode.Auth
	case http.StatusNotFound:
		return exitcode.NotFound
	default:
		return exitcode.API
	}
}

// parseError decodes NPM's error envelope. debug.stack is deliberately dropped:
// it is present in NPM responses and carries absolute server paths that have no
// place in CLI output.
func parseError(status int, method, path string, body []byte) error {
	var env struct {
		Error *APIError `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err == nil && env.Error != nil && env.Error.Message != "" {
		env.Error.Status = status
		env.Error.Method = method
		env.Error.Path = path
		return env.Error
	}
	msg := string(body)
	if len(msg) > 512 {
		msg = msg[:512] + "..."
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &APIError{Status: status, Code: status, Message: msg, Method: method, Path: path}
}

// IsNotFound reports whether err is a 404 from NPM.
func IsNotFound(err error) bool {
	var ae *APIError
	if asErr(err, &ae) {
		return ae.Status == http.StatusNotFound
	}
	return false
}
