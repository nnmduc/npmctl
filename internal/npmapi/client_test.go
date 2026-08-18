package npmapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tlsFixture starts an HTTPS server with a self-signed certificate — the ordinary
// homelab NPM setup — and writes that certificate to a PEM file.
func tlsFixture(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	}))
	t.Cleanup(srv.Close)

	caPath := filepath.Join(t.TempDir(), "lab-ca.pem")
	block := &pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}
	if err := os.WriteFile(caPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return srv, caPath
}

// TestSelfSignedFailsByDefault establishes the baseline: verification is on, so an
// untrusted certificate is rejected rather than silently accepted.
func TestSelfSignedFailsByDefault(t *testing.T) {
	srv, _ := tlsFixture(t)
	c, err := New(Options{BaseURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListProxyHosts(context.Background()); err == nil {
		t.Fatal("a self-signed certificate must not be trusted by default")
	}
}

// TestCACertValidatesWithoutInsecure is the point of R11: the common homelab case
// is solvable while keeping verification ON.
func TestCACertValidatesWithoutInsecure(t *testing.T) {
	srv, caPath := tlsFixture(t)
	c, err := New(Options{BaseURL: srv.URL, Token: "t", CACert: caPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListProxyHosts(context.Background()); err != nil {
		t.Fatalf("--ca-cert should validate the endpoint without --insecure: %v", err)
	}
}

// TestPinSHA256Validates offers the second verification-preserving option.
func TestPinSHA256Validates(t *testing.T) {
	srv, _ := tlsFixture(t)
	sum := sha256.Sum256(srv.Certificate().RawSubjectPublicKeyInfo)
	pin := base64.StdEncoding.EncodeToString(sum[:])

	c, err := New(Options{BaseURL: srv.URL, Token: "t", PinSHA256: pin})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListProxyHosts(context.Background()); err != nil {
		t.Fatalf("a correct pin should validate: %v", err)
	}
}

// TestWrongPinIsRejected proves pinning is a real check rather than a way to skip
// verification: it sets InsecureSkipVerify internally, so a wrong pin MUST fail.
func TestWrongPinIsRejected(t *testing.T) {
	srv, _ := tlsFixture(t)
	wrong := base64.StdEncoding.EncodeToString(make([]byte, sha256.Size))

	c, err := New(Options{BaseURL: srv.URL, Token: "t", PinSHA256: wrong})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListProxyHosts(context.Background()); err == nil {
		t.Fatal("a mismatched pin must reject the connection")
	}
}

func TestInsecureSkipsVerification(t *testing.T) {
	srv, _ := tlsFixture(t)
	c, err := New(Options{BaseURL: srv.URL, Token: "t", Insecure: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListProxyHosts(context.Background()); err != nil {
		t.Fatalf("--insecure should connect anyway: %v", err)
	}
}

// TestPathsAreApiPrefixed catches an easy off-by-one in URL joining: NPM serves
// the API under /api, and the vendored schema's paths are relative to it.
func TestPathsAreApiPrefixed(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	c, _ := New(Options{BaseURL: srv.URL, Token: "t"})
	if _, err := c.ListProxyHosts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got != "/api/nginx/proxy-hosts" {
		t.Fatalf("request path = %q, want /api/nginx/proxy-hosts", got)
	}
}

// TestBaseURLWithSubpath supports NPM behind a reverse proxy on a sub-path.
func TestBaseURLWithSubpath(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	c, _ := New(Options{BaseURL: srv.URL + "/npm/", Token: "t"})
	if _, err := c.ListProxyHosts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got != "/npm/api/nginx/proxy-hosts" {
		t.Fatalf("request path = %q, want /npm/api/nginx/proxy-hosts", got)
	}
}

func TestMissingURLIsUsageError(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("an empty base URL must be rejected")
	}
	if _, err := New(Options{BaseURL: "npm.example.com"}); err == nil {
		t.Fatal("a URL with no scheme must be rejected")
	}
}

// TestVerboseLogRedactsBearer covers the -v path, which prints request metadata
// including headers.
func TestVerboseLogRedactsBearer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"expires":"x","token":"super-secret-token-value"}`)
	}))
	defer srv.Close()

	var log strings.Builder
	c, _ := New(Options{BaseURL: srv.URL, Token: "bearer-secret-abc", Verbose: true, VerboseTo: &log})
	if _, err := c.MintToken(context.Background(), "30d"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(log.String(), "super-secret-token-value") {
		t.Errorf("-v leaked the response token:\n%s", log.String())
	}
	if strings.Contains(log.String(), "bearer-secret-abc") {
		t.Errorf("-v leaked the request bearer:\n%s", log.String())
	}
}
