package cli

import (
	"net/http"
	"strings"
	"testing"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

// TestLoginRefusesPlaintextByDefault: over http:// the password AND every later bearer
// token cross the network in cleartext. A LAN is not a private channel.
func TestLoginRefusesPlaintextByDefault(t *testing.T) {
	h := newHarness(t) // httptest serves plain HTTP
	h.route("POST", "/api/tokens", http.StatusOK,
		map[string]any{"expires": "2026-09-17T00:00:00.000Z", "token": "should-never-be-issued"})

	t.Setenv("NPMCTL_PASSWORD", "hunter2")
	_, stderr, code := h.run("auth", "login", "--identity", "me@example.com")

	if code != exitcode.Refused {
		t.Fatalf("want exit %d over plain HTTP, got %d\n%s", exitcode.Refused, code, stderr)
	}
	if muts := h.mutations(); len(muts) != 0 {
		t.Fatalf("the password was sent anyway: %+v", muts)
	}
	if !strings.Contains(stderr, "--allow-plaintext") {
		t.Errorf("the refusal must name the explicit opt-out: %s", stderr)
	}
}

// TestLoginAllowsPlaintextWithExplicitFlag keeps the escape hatch, loudly.
func TestLoginAllowsPlaintextWithExplicitFlag(t *testing.T) {
	h := newHarness(t)
	h.route("POST", "/api/tokens", http.StatusOK,
		map[string]any{"expires": "2026-09-17T00:00:00.000Z", "token": "login-token"})
	h.route("GET", "/api/tokens", http.StatusOK,
		map[string]any{"expires": "2026-09-17T00:00:00.000Z", "token": "minted-30d-token"})

	t.Setenv("NPMCTL_PASSWORD", "hunter2")
	stdout, stderr, code := h.run("auth", "login", "--identity", "me@example.com", "--allow-plaintext")

	if code != exitcode.OK {
		t.Fatalf("want exit 0 with --allow-plaintext, got %d\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "without transport protection") {
		t.Errorf("the risk must be stated on stderr: %s", stderr)
	}
	// The 30-day token is minted via GET /tokens?expiry=, not the login token.
	if h.countRequests("GET", "/api/tokens") != 1 {
		t.Error("login should mint a bounded token after authenticating")
	}
	if strings.Contains(stdout, "hunter2") || strings.Contains(stdout, "minted-30d-token") {
		t.Errorf("login output leaked a secret:\n%s", stdout)
	}
	if got := decodeJSON(t, stdout)["password_stored"]; got != false {
		t.Errorf("password_stored = %v, want false by default", got)
	}
}

// TestIsPlaintextURL covers the classification, including the missing-scheme case:
// guessing "probably https" on a credential path is the wrong default.
func TestIsPlaintextURL(t *testing.T) {
	cases := map[string]bool{
		"https://npm.example.com": false,
		"HTTPS://npm.example.com": false,
		"http://npm.example.com":  true,
		"http://10.161.206.88:81": true,
		"npm.example.com":         true,
		"":                        true,
	}
	for raw, want := range cases {
		if got := isPlaintextURL(raw); got != want {
			t.Errorf("isPlaintextURL(%q) = %v, want %v", raw, got, want)
		}
	}
}
