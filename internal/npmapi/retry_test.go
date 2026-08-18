package npmapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// counter records how many times each method+path was actually put on the wire.
type counter struct {
	mu     sync.Mutex
	counts map[string]int
}

func newCounter() *counter { return &counter{counts: map[string]int{}} }

func (c *counter) add(method, path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[method+" "+path]++
}

func (c *counter) get(method, path string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[method+" "+path]
}

// TestMutatingMethodsAreNeverRetried is R4's regression guard.
//
// NPM commits to its database and regenerates nginx config before responding, so
// a retried POST is a second create — and a retried certificate order spends
// another of Let's Encrypt's five duplicate certificates per week. The server
// here answers 502 every time, which is exactly the condition a naive transport
// would retry on.
func TestMutatingMethodsAreNeverRetried(t *testing.T) {
	cnt := newCounter()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cnt.add(r.Method, r.URL.Path)
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, `{"error":{"code":502,"message":"bad gateway"}}`)
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	mutations := []struct {
		name string
		call func() error
	}{
		{"POST create", func() error { _, e := c.CreateProxyHost(ctx, map[string]any{"forward_port": 80}); return e }},
		{"PUT update", func() error { _, e := c.UpdateProxyHost(ctx, 1, map[string]any{"forward_port": 80}); return e }},
		{"DELETE", func() error { return c.DeleteProxyHost(ctx, 1) }},
		{"POST enable", func() error { return c.EnableProxyHost(ctx, 1) }},
		{"POST disable", func() error { return c.DisableProxyHost(ctx, 1) }},
	}
	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			before := totalRequests(cnt)
			if err := m.call(); err == nil {
				t.Fatal("expected an error from a 502 response")
			}
			if sent := totalRequests(cnt) - before; sent != 1 {
				t.Fatalf("%s was sent %d times; a mutating method must be attempted exactly once", m.name, sent)
			}
		})
	}
}

// TestReadsAreRetried is the other half of the allowlist: a GET is safe to repeat,
// and a 502 from a proxy in front of NPM usually means the request never arrived.
func TestReadsAreRetried(t *testing.T) {
	cnt := newCounter()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cnt.add(r.Method, r.URL.Path)
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, `{"error":{"code":502,"message":"bad gateway"}}`)
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListProxyHosts(context.Background()); err == nil {
		t.Fatal("expected an error after retries were exhausted")
	}
	if got := cnt.get("GET", "/api/nginx/proxy-hosts"); got != maxAttempts {
		t.Fatalf("GET was attempted %d times, want %d", got, maxAttempts)
	}
}

// TestRetryStopsOnSuccess proves a retried read returns the eventual success
// rather than the first failure.
func TestRetryStopsOnSuccess(t *testing.T) {
	cnt := newCounter()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cnt.add(r.Method, r.URL.Path)
		if cnt.get(r.Method, r.URL.Path) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	c, err := New(Options{BaseURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListProxyHosts(context.Background()); err != nil {
		t.Fatalf("second attempt should have succeeded: %v", err)
	}
}

// TestNonRetryableStatusIsNotRetried: a 400 or 404 is NPM's considered answer, and
// repeating the request cannot change it.
func TestNonRetryableStatusIsNotRetried(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			cnt := newCounter()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				cnt.add(r.Method, r.URL.Path)
				w.WriteHeader(status)
				fmt.Fprintf(w, `{"error":{"code":%d,"message":"no"}}`, status)
			}))
			defer srv.Close()

			c, _ := New(Options{BaseURL: srv.URL, Token: "t"})
			_, _ = c.ListProxyHosts(context.Background())
			if got := cnt.get("GET", "/api/nginx/proxy-hosts"); got != 1 {
				t.Fatalf("status %d was retried %d times; only 502/503/504 are retryable", status, got)
			}
		})
	}
}

// TestIsMutatingClassifiesEveryMethod locks the predicate the whole control rests
// on, so a future method cannot default into being retryable.
func TestIsMutatingClassifiesEveryMethod(t *testing.T) {
	safe := []string{http.MethodGet, http.MethodHead, http.MethodOptions}
	unsafe := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, "PROPFIND"}
	for _, m := range safe {
		if isMutating(m) {
			t.Errorf("%s should be treated as safe to retry", m)
		}
	}
	for _, m := range unsafe {
		if !isMutating(m) {
			t.Errorf("%s must be treated as mutating", m)
		}
	}
}

func totalRequests(c *counter) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, v := range c.counts {
		n += v
	}
	return n
}
