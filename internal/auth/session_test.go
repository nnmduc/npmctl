package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

// stubMinter records refresh attempts.
type stubMinter struct {
	calls int
	tok   *Token
	err   error
}

func (s *stubMinter) MintToken(_ context.Context, _ string) (*Token, error) {
	s.calls++
	return s.tok, s.err
}

// memStore is an in-memory Store for assertions.
type memStore struct{ saved *Credential }

func (m *memStore) Load(_, _, _ string) (*Credential, error) { return m.saved, nil }
func (m *memStore) Save(c *Credential) error                 { m.saved = c; return nil }
func (m *memStore) Delete(_, _, _ string) error              { return nil }
func (m *memStore) Backend() string                          { return "memory" }

func iso(d time.Duration) string {
	return time.Now().Add(d).UTC().Format(time.RFC3339Nano)
}

// TestExpiredTokenRequiresReauth is the core of the "no automatic re-login" rule:
// re-login is impossible on a 2FA account, and with a stored password it degrades
// into spraying production on every call. Exit 9 sends the human to auth login.
func TestExpiredTokenRequiresReauth(t *testing.T) {
	r := &Resolved{Credential: &Credential{
		Identity: "me@example.com", Token: "old", Expires: iso(-time.Hour),
	}}
	m := &stubMinter{}

	_, err := r.Ensure(context.Background(), m, &memStore{}, time.Now())
	if err == nil {
		t.Fatal("an expired token must not be used")
	}
	if got := exitcode.Of(err); got != exitcode.ReauthRequired {
		t.Errorf("exit code = %d, want %d (re-authentication required)", got, exitcode.ReauthRequired)
	}
	if m.calls != 0 {
		t.Error("an expired token must not trigger a silent re-login attempt")
	}
}

// TestTokenWithoutExpiryIsTrusted covers --token and NPMCTL_TOKEN: we are handed
// the secret, not its metadata. Treating unknown expiry as expired would break
// every CI and container invocation before it sent a request.
func TestTokenWithoutExpiryIsTrusted(t *testing.T) {
	r := &Resolved{Credential: &Credential{Token: "env-token"}}

	got, err := r.Ensure(context.Background(), &stubMinter{}, nil, time.Now())
	if err != nil {
		t.Fatalf("a token with no known expiry should be used: %v", err)
	}
	if got.Token != "env-token" {
		t.Errorf("token = %q", got.Token)
	}
}

// TestNearExpiryRefreshes exercises the opportunistic refresh that keeps a
// long-lived credential alive without human involvement.
func TestNearExpiryRefreshes(t *testing.T) {
	store := &memStore{}
	r := &Resolved{Credential: &Credential{
		Profile: "prod", Identity: "me@example.com", Token: "old", Expires: iso(24 * time.Hour),
	}}
	m := &stubMinter{tok: &Token{Token: "renewed", Expires: iso(30 * 24 * time.Hour)}}

	got, err := r.Ensure(context.Background(), m, store, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if m.calls != 1 {
		t.Fatalf("expected one refresh attempt, got %d", m.calls)
	}
	if got.Token != "renewed" {
		t.Errorf("token = %q, want the renewed one", got.Token)
	}
	if store.saved == nil || store.saved.Token != "renewed" {
		t.Error("the renewed token was not persisted")
	}
}

// TestFailedRefreshKeepsWorkingToken: a refresh failure must not break a call that
// the current, still-valid token can serve.
func TestFailedRefreshKeepsWorkingToken(t *testing.T) {
	r := &Resolved{Credential: &Credential{
		Identity: "me@example.com", Token: "current", Expires: iso(24 * time.Hour),
	}}
	m := &stubMinter{err: errors.New("network down")}

	got, err := r.Ensure(context.Background(), m, &memStore{}, time.Now())
	if err != nil {
		t.Fatalf("a failed refresh should not fail the command: %v", err)
	}
	if got.Token != "current" {
		t.Errorf("token = %q, want the existing valid one", got.Token)
	}
}

// TestHealthyTokenIsNotRefreshed avoids a needless round-trip on every command.
func TestHealthyTokenIsNotRefreshed(t *testing.T) {
	r := &Resolved{Credential: &Credential{
		Identity: "me@example.com", Token: "fine", Expires: iso(20 * 24 * time.Hour),
	}}
	m := &stubMinter{}
	if _, err := r.Ensure(context.Background(), m, &memStore{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if m.calls != 0 {
		t.Errorf("a token far from expiry was refreshed %d time(s)", m.calls)
	}
}

// TestUnparseableExpiryIsTreatedAsExpired: a stored credential with a garbled
// expiry must fail closed, not be trusted forever.
func TestUnparseableExpiryIsTreatedAsExpired(t *testing.T) {
	c := &Credential{Token: "t", Expires: "not-a-date"}
	if !c.Expired(time.Now()) {
		t.Error("an unparseable stored expiry must be treated as expired")
	}
}

// TestResolutionOrder pins the documented precedence.
func TestResolutionOrder(t *testing.T) {
	t.Setenv(EnvToken, "from-env")
	r := &Resolver{Profile: "prod", URL: "https://npm.example.com", Identity: "me@x.com", FlagToken: "from-flag"}

	got, err := r.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if got.Credential.Token != "from-flag" {
		t.Errorf("--token must outrank the environment, got %q", got.Credential.Token)
	}

	r.FlagToken = ""
	got, err = r.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if got.Credential.Token != "from-env" {
		t.Errorf("NPMCTL_TOKEN should be next, got %q", got.Credential.Token)
	}
}

// TestNpmTokenIsNotHonoured: NPM_TOKEN belongs to the npm registry, and reading it
// would let an unrelated CI secret authenticate against a proxy manager.
func TestNpmTokenIsNotHonoured(t *testing.T) {
	t.Setenv("NPM_TOKEN", "registry-secret")
	t.Setenv(EnvToken, "")

	if c := EnvCredential("prod", "https://npm.example.com", "me@x.com"); c != nil {
		t.Errorf("NPM_TOKEN must be ignored, but a credential was resolved: %+v", c)
	}
}

func TestWriteAllowedRequiresExactlyOne(t *testing.T) {
	for _, tc := range []struct {
		val  string
		want bool
	}{{"1", true}, {"", false}, {"0", false}, {"true", false}, {"yes", false}} {
		t.Setenv(EnvAllowWrite, tc.val)
		if got := WriteAllowed(); got != tc.want {
			t.Errorf("%s=%q → %v, want %v", EnvAllowWrite, tc.val, got, tc.want)
		}
	}
}
