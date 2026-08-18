package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/nnmduc/npmctl/internal/auth"
	"github.com/nnmduc/npmctl/internal/exitcode"
)

// recorded is one request the fake NPM instance received. Tests assert on the
// METHOD, which is how "--dry-run issues no mutating request" is proven by
// method rather than by counting requests.
type recorded struct {
	Method string
	Path   string
	Body   string
}

// harness runs the real command tree against a fake NPM instance, with an
// isolated HOME so the credential file, config and undo journal never touch the
// developer's own.
type harness struct {
	t        *testing.T
	server   *httptest.Server
	mu       sync.Mutex
	requests []recorded
	routes   map[string]http.HandlerFunc
	home     string
	stdin    string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, routes: map[string]http.HandlerFunc{}, home: t.TempDir()}

	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(r.Body)
		h.mu.Lock()
		h.requests = append(h.requests, recorded{Method: r.Method, Path: r.URL.Path, Body: body.String()})
		h.mu.Unlock()
		// Recording consumed the body; hand a fresh reader to the route handler so
		// it can decode the payload too.
		r.Body = io.NopCloser(bytes.NewReader(body.Bytes()))

		if fn, ok := h.routes[r.Method+" "+r.URL.Path]; ok {
			fn(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"code":404,"message":"not found"}}`)
	}))
	t.Cleanup(h.server.Close)

	// Isolate every on-disk side effect.
	t.Setenv("HOME", h.home)
	t.Setenv("XDG_CONFIG_HOME", h.home+"/.config")
	t.Setenv("XDG_STATE_HOME", h.home+"/.state")
	t.Setenv("XDG_DATA_HOME", h.home+"/.data")
	// A stored token would otherwise be looked up in the real OS keyring. Tests
	// also override HOME, and macOS derives the login keychain path from it — so
	// any keyring call would fail to find a keychain and raise a GUI prompt.
	// Forcing the file backend keeps the suite headless and non-interactive.
	t.Setenv(auth.EnvNoKeyring, "1")
	t.Setenv(auth.EnvToken, "test-token")
	t.Setenv(auth.EnvAllowWrite, "")
	return h
}

// route registers a JSON response for one method and path.
func (h *harness) route(method, path string, status int, payload any) {
	h.routes[method+" "+path] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if payload != nil {
			_ = json.NewEncoder(w).Encode(payload)
		}
	}
}

// routeFunc registers an arbitrary handler.
func (h *harness) routeFunc(method, path string, fn http.HandlerFunc) {
	h.routes[method+" "+path] = fn
}

// run executes npmctl with the given args and returns stdout, stderr and the
// exit code the process would have used.
func (h *harness) run(args ...string) (string, string, int) {
	h.t.Helper()
	var out, errb bytes.Buffer
	restore := SetStreams(&out, &errb, strings.NewReader(h.stdin))
	defer restore()

	root, _ := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&errb)
	full := append([]string{"--url", h.server.URL}, args...)
	root.SetArgs(full)

	err := root.Execute()
	code := exitcode.OK
	if err != nil {
		code = exitcode.Of(err)
		fmt.Fprintf(&errb, "error: %v\n", err)
	}
	return out.String(), errb.String(), code
}

// allowWrites sets the out-of-argv factor.
func (h *harness) allowWrites() { h.t.Setenv(auth.EnvAllowWrite, "1") }

// mutations returns every non-idempotent request the server received. This is
// the assertion the dry-run guarantee rests on.
func (h *harness) mutations() []recorded {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []recorded
	for _, r := range h.requests {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			out = append(out, r)
		}
	}
	return out
}

func (h *harness) countRequests(method, path string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.requests {
		if r.Method == method && r.Path == path {
			n++
		}
	}
	return n
}

// sampleHost is a proxy-host fixture with a healthy nginx state.
func sampleHost() map[string]any {
	return map[string]any{
		"id":                      12,
		"created_on":              "2026-01-01T00:00:00.000Z",
		"modified_on":             "2026-01-02T00:00:00.000Z",
		"owner_user_id":           1,
		"domain_names":            []string{"app.example.com"},
		"forward_scheme":          "http",
		"forward_host":            "10.0.0.9",
		"forward_port":            8080,
		"certificate_id":          3,
		"ssl_forced":              true,
		"hsts_enabled":            false,
		"hsts_subdomains":         false,
		"http2_support":           true,
		"block_exploits":          true,
		"caching_enabled":         false,
		"allow_websocket_upgrade": true,
		"trust_forwarded_proto":   false,
		"access_list_id":          0,
		"advanced_config":         "",
		"enabled":                 true,
		"locations":               []any{},
		"meta":                    map[string]any{"nginx_online": true, "nginx_err": nil},
	}
}

func decodeJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, s)
	}
	return m
}
