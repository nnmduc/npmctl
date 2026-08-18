// Request execution: URL construction, body encoding, response decoding and the
// redacted verbose transport log.
package npmapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/nnmduc/npmctl/internal/exitcode"
	"github.com/nnmduc/npmctl/internal/output"
)

// request is one API call.
type request struct {
	method string
	path   string // API path WITHOUT the /api prefix, e.g. "/nginx/proxy-hosts"
	query  url.Values
	body   any      // JSON-encoded when non-nil
	raw    *rawBody // pre-encoded body (multipart), used instead of body
	noAuth bool
}

type rawBody struct {
	contentType string
	data        []byte
}

// do executes a request and decodes a JSON response into out.

// do executes a request and decodes a JSON response into out.
func (c *Client) do(ctx context.Context, r request, out any) error {
	endpoint := *c.base
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/api" + r.path
	if len(r.query) > 0 {
		endpoint.RawQuery = r.query.Encode()
	}

	var reader io.Reader
	contentType := ""
	switch {
	case r.raw != nil:
		reader = bytes.NewReader(r.raw.data)
		contentType = r.raw.contentType
		// Never log a raw body. The scrubber keys off decoded field names, so a
		// multipart payload carrying privkey.pem would bypass it entirely.
		c.logf("--> %s %s <%s, %d bytes, body not logged>", r.method, endpoint.Path, contentType, len(r.raw.data))
	case r.body != nil:
		b, err := json.Marshal(r.body)
		if err != nil {
			return exitcode.Wrap(exitcode.Generic, err, "encode request body")
		}
		reader = bytes.NewReader(b)
		contentType = "application/json"
		c.logf("--> %s %s %s", r.method, endpoint.Path, mustScrubJSON(b))
	}
	if r.body == nil && r.raw == nil {
		c.logf("--> %s %s", r.method, endpoint.Path)
	}

	req, err := http.NewRequestWithContext(ctx, r.method, endpoint.String(), reader)
	if err != nil {
		return exitcode.Wrap(exitcode.Generic, err, "build request")
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	if !r.noAuth && c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// A transport failure on a mutating method is genuinely ambiguous: NPM
		// commits before responding, so a lost response is indistinguishable
		// from a write that never happened. Exit 7 says exactly that.
		if isMutating(r.method) {
			return exitcode.Wrap(exitcode.Network, err,
				"%s %s failed in transit — the write MAY have been applied; verify with a read before retrying",
				r.method, r.path)
		}
		return exitcode.Wrap(exitcode.Network, err, "%s %s", r.method, r.path)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return exitcode.Wrap(exitcode.Network, err, "read response body")
	}
	c.logf("<-- %d %s %s", resp.StatusCode, r.path, truncate(mustScrubJSON(body), 2048))

	if resp.StatusCode >= 400 {
		return parseError(resp.StatusCode, r.method, r.path, body)
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return exitcode.Wrap(exitcode.API, err, "decode %s %s response", r.method, r.path)
	}
	return nil
}

// doRaw executes a request and returns the response bytes undecoded, for
// endpoints that answer with a zip rather than JSON.

// doRaw executes a request and returns the response bytes undecoded, for
// endpoints that answer with a zip rather than JSON.
func (c *Client) doRaw(ctx context.Context, r request) ([]byte, string, error) {
	endpoint := *c.base
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/api" + r.path
	if len(r.query) > 0 {
		endpoint.RawQuery = r.query.Encode()
	}
	c.logf("--> %s %s", r.method, endpoint.Path)
	req, err := http.NewRequestWithContext(ctx, r.method, endpoint.String(), nil)
	if err != nil {
		return nil, "", exitcode.Wrap(exitcode.Generic, err, "build request")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", exitcode.Wrap(exitcode.Network, err, "%s %s", r.method, r.path)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", exitcode.Wrap(exitcode.Network, err, "read response body")
	}
	if resp.StatusCode >= 400 {
		return nil, "", parseError(resp.StatusCode, r.method, r.path, body)
	}
	return body, resp.Header.Get("Content-Type"), nil
}

// logf writes a verbose transport line. Bodies and headers pass through the
// output scrubber first: -v is a documented way to leak a bearer token
// otherwise.

// logf writes a verbose transport line. Bodies and headers pass through the
// output scrubber first: -v is a documented way to leak a bearer token
// otherwise.
func (c *Client) logf(format string, args ...any) {
	if !c.verbose {
		return
	}
	fmt.Fprintln(c.vw, output.Scrub(fmt.Sprintf(format, args...)))
}

// mustScrubJSON redacts a JSON payload for logging. Unparseable bodies are
// reduced to a length note rather than echoed: an unparseable body is exactly
// the case where we cannot reason about what it contains.

// mustScrubJSON redacts a JSON payload for logging. Unparseable bodies are
// reduced to a length note rather than echoed: an unparseable body is exactly
// the case where we cannot reason about what it contains.
func mustScrubJSON(b []byte) string {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return fmt.Sprintf("<%d bytes>", len(b))
	}
	out, err := json.Marshal(output.Scrub(v))
	if err != nil {
		return fmt.Sprintf("<%d bytes>", len(b))
	}
	return string(out)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
