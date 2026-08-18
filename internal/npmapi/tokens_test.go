package npmapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRequestTokenReturnsChallengeOn2FA covers the oneOf branch. NPM answers the
// challenge at HTTP 200, so a client that only inspected the status code would
// treat a 2FA account as a successful login with an empty token.
func TestRequestTokenReturnsChallengeOn2FA(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"requires_2fa":true,"challenge_token":"challenge-abc"}`)
	}))
	defer srv.Close()

	c, _ := New(Options{BaseURL: srv.URL})
	tok, challenge, err := c.RequestToken(context.Background(), "me@example.com", "pw")
	if err != nil {
		t.Fatal(err)
	}
	if tok != nil {
		t.Fatalf("expected no token on a 2FA challenge, got %+v", tok)
	}
	if challenge == nil || challenge.ChallengeToken != "challenge-abc" {
		t.Fatalf("challenge not parsed: %+v", challenge)
	}
}

func TestRequestTokenSendsIdentityAndSecret(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"expires":"2026-09-17T00:00:00.000Z","token":"tok"}`)
	}))
	defer srv.Close()

	c, _ := New(Options{BaseURL: srv.URL})
	if _, _, err := c.RequestToken(context.Background(), "me@example.com", "pw"); err != nil {
		t.Fatal(err)
	}
	// The schema names these identity/secret, not email/password. Getting this
	// wrong produces a 400 on the very first call the tool ever makes.
	if body["identity"] != "me@example.com" {
		t.Errorf(`body["identity"] = %v, want the email`, body["identity"])
	}
	if body["secret"] != "pw" {
		t.Errorf(`body["secret"] = %v, want the password`, body["secret"])
	}
	if _, ok := body["email"]; ok {
		t.Error(`body must not contain "email" — the schema forbids unknown properties`)
	}
	if _, ok := body["password"]; ok {
		t.Error(`body must not contain "password" — the schema forbids unknown properties`)
	}
}

// TestVerify2FAPreservesLeadingZeros is the reason code is a string end to end.
// The schema types it as a 6-8 character string, and a TOTP code beginning with a
// zero is perfectly ordinary — parsing it as an integer silently sends 12345.
func TestVerify2FAPreservesLeadingZeros(t *testing.T) {
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"expires":"2026-09-17T00:00:00.000Z","token":"tok"}`)
	}))
	defer srv.Close()

	c, _ := New(Options{BaseURL: srv.URL})
	if _, err := c.Verify2FA(context.Background(), "challenge-abc", "012345"); err != nil {
		t.Fatal(err)
	}
	if !contains(raw, `"code":"012345"`) {
		t.Fatalf("2FA code must be sent as the string 012345, got body: %s", raw)
	}
}

// TestMintTokenSendsExpiryQuery is the mechanism the whole credential design rests
// on: POST /tokens has no expiry parameter, but the unvalidated GET /tokens
// honours one, which is what makes a bounded 30-day token possible.
func TestMintTokenSendsExpiryQuery(t *testing.T) {
	var gotExpiry, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotExpiry = r.URL.Query().Get("expiry")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"expires":"2026-09-17T00:00:00.000Z","token":"minted"}`)
	}))
	defer srv.Close()

	c, _ := New(Options{BaseURL: srv.URL, Token: "existing"})
	tok, err := c.MintToken(context.Background(), "30d")
	if err != nil {
		t.Fatal(err)
	}
	if gotExpiry != "30d" {
		t.Errorf("expiry query = %q, want 30d", gotExpiry)
	}
	if gotAuth != "Bearer existing" {
		t.Errorf("refresh must present the current bearer, got %q", gotAuth)
	}
	if tok.Token != "minted" {
		t.Errorf("token = %q, want minted", tok.Token)
	}
}

func TestHealthIsUnauthenticated(t *testing.T) {
	var sawAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization") != ""
		if r.URL.Path != "/api/" {
			t.Errorf("health must call /api/, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"OK","setup":true,"version":{"major":2,"minor":15,"revision":1}}`)
	}))
	defer srv.Close()

	c, _ := New(Options{BaseURL: srv.URL, Token: "should-not-be-sent"})
	h, err := c.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sawAuth {
		t.Error("the health probe must not present a credential")
	}
	if h.Status != "OK" || h.Version.Minor != 15 {
		t.Errorf("health not parsed: %+v", h)
	}
}

// TestAuthFailureMapsToExitFour keeps the exit-code contract honest: an agent
// must be able to tell "your credential is bad" from "your request is bad".
func TestAuthFailureMapsToExitFour(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			fmt.Fprint(w, `{"error":{"code":401,"message":"denied"}}`)
		}))
		c, _ := New(Options{BaseURL: srv.URL, Token: "bad"})
		_, err := c.ListProxyHosts(context.Background())
		var ae *APIError
		if !asErr(err, &ae) {
			srv.Close()
			t.Fatalf("expected an APIError, got %v", err)
		}
		if ae.ExitCode() != 4 {
			t.Errorf("status %d mapped to exit %d, want 4", status, ae.ExitCode())
		}
		srv.Close()
	}
}

// TestServerStackTraceIsNotSurfaced: NPM includes debug.stack with absolute server
// paths, which has no place in CLI output.
func TestServerStackTraceIsNotSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"code":400,"message":"Domains are invalid","debug":{"stack":["/app/internal/proxy-host.js:94"]}}}`)
	}))
	defer srv.Close()

	c, _ := New(Options{BaseURL: srv.URL, Token: "t"})
	_, err := c.ListProxyHosts(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if contains(err.Error(), "proxy-host.js") {
		t.Errorf("server stack trace leaked into the error: %v", err)
	}
	if !contains(err.Error(), "Domains are invalid") {
		t.Errorf("the useful message was lost: %v", err)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
