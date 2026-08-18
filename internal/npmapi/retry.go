package npmapi

import (
	"net/http"
	"strings"
	"time"
)

// isMutating reports whether a method commits state on the server.
//
// This predicate is the whole of R4. NPM writes to its database and regenerates
// nginx config BEFORE it answers, so a retried POST is a second create, not a
// second attempt at the first one — and a retried POST /nginx/certificates
// spends another of Let's Encrypt's 5 duplicate certificates per week.
func isMutating(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

// retryableStatus lists the statuses worth a second attempt: all three mean a
// proxy in front of NPM answered, not NPM itself, so the request very likely
// never reached the application.
func retryableStatus(code int) bool {
	switch code {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

const (
	maxAttempts  = 2
	retryBackoff = 500 * time.Millisecond
)

// retryTransport retries idempotent requests only.
//
// The allowlist is expressed as a property of the request method rather than as
// a per-call-site decision, so a future endpoint cannot opt itself into retrying
// a mutation by forgetting a flag. There is deliberately no configuration knob
// to widen it.
type retryTransport struct {
	next http.RoundTripper

	// attempts counts every outbound request, including retries. Tests read it
	// to prove a mutating method was issued exactly once.
	attempts int
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// A request with a body cannot be replayed safely in general; every method
	// we retry is bodyless, so this is belt-and-braces alongside isMutating.
	retryable := !isMutating(req.Method) && req.Body == nil

	var lastResp *http.Response
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		t.attempts++
		resp, err := t.next.RoundTrip(req)
		if !retryable {
			return resp, err
		}
		if err == nil && !retryableStatus(resp.StatusCode) {
			return resp, nil
		}
		lastResp, lastErr = resp, err
		if attempt < maxAttempts {
			if resp != nil {
				// Drain so the connection can be reused rather than leaked.
				resp.Body.Close()
				lastResp = nil
			}
			time.Sleep(retryBackoff)
		}
	}
	return lastResp, lastErr
}
