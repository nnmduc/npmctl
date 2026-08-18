package auth

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

// Minter refreshes a token. Implemented by *npmapi.Client; an interface here
// keeps the auth package free of an import cycle and testable without a server.
type Minter interface {
	MintToken(ctx context.Context, expiry string) (*Token, error)
}

// Token mirrors npmapi.Token without importing it.
type Token struct {
	Expires string
	Token   string
}

// Resolved is the credential a command will actually use, plus where it came from.
type Resolved struct {
	Credential *Credential
	Backend    string
}

// Resolver walks the credential resolution chain.
type Resolver struct {
	Profile  string
	URL      string
	Identity string

	// FlagToken is --token: explicit, one-shot, never persisted.
	FlagToken string

	Keyring *KeyringStore
	File    *FileStore
	Warn    io.Writer
}

// Resolve returns the credential to use, in precedence order:
//
//  1. --token         explicit and ephemeral
//  2. NPMCTL_TOKEN    CI, containers, headless
//  3. OS keyring      interactive desktop default
//  4. 0600 file       explicit fallback when no keyring exists
func (r *Resolver) Resolve() (*Resolved, error) {
	if r.FlagToken != "" {
		return &Resolved{
			Credential: &Credential{Profile: r.Profile, URL: r.URL, Identity: r.Identity, Token: r.FlagToken},
			Backend:    "--token flag",
		}, nil
	}
	if c := EnvCredential(r.Profile, r.URL, r.Identity); c != nil {
		return &Resolved{Credential: c, Backend: EnvToken + " env"}, nil
	}
	if r.Keyring != nil && r.Keyring.Available() {
		c, err := r.Keyring.Load(r.Profile, r.URL, r.Identity)
		if err == nil {
			return &Resolved{Credential: c, Backend: r.Keyring.Backend()}, nil
		}
		if !IsNotFound(err) {
			return nil, err
		}
	}
	if r.File != nil {
		c, err := r.File.Load(r.Profile, r.URL, r.Identity)
		if err == nil {
			return &Resolved{Credential: c, Backend: r.File.Backend()}, nil
		}
		if !IsNotFound(err) {
			return nil, err
		}
	}
	// With no URL configured at all this is a fresh install, not an expired session, so
	// the guidance differs: there is nothing to re-authenticate against yet.
	if strings.TrimSpace(r.URL) == "" {
		return nil, exitcode.New(exitcode.ReauthRequired,
			"profile %q has no NPM url configured — run `npmctl auth login --url https://npm.example.com`",
			r.Profile)
	}
	return nil, exitcode.New(exitcode.ReauthRequired,
		"no credential for profile %q at %s — run `npmctl auth login --url %s`", r.Profile, r.URL, r.URL)
}

// warnedFallback keeps the keyring-fallback notice to once per process. It is a
// one-time warning, not a per-command banner.
var warnedFallback sync.Once

// PreferredStore returns the store a login should write to, warning once when the
// keyring is unreachable and the 0600 file is used instead.
func (r *Resolver) PreferredStore() Store {
	if r.Keyring != nil && r.Keyring.Available() {
		return r.Keyring
	}
	// Silent when the operator explicitly opted out: they already know.
	if r.Warn != nil && os.Getenv(EnvNoKeyring) != "1" {
		warnedFallback.Do(func() {
			fmt.Fprintf(r.Warn,
				"warning: no OS keyring available; storing the token in %s (mode 0600)\n", r.File.Path)
		})
	}
	return r.File
}

// Ensure returns a usable token, refreshing opportunistically.
//
// It deliberately does NOT re-login. Automatic re-login is impossible on a 2FA
// account — POST /tokens answers with a challenge, not a token — and with a
// stored password it degrades into spraying production credentials on every
// invocation after a rotation. Exit 9 tells the human to run `auth login`
// instead of failing in a way an agent would retry.
func (r *Resolved) Ensure(ctx context.Context, m Minter, save Store, now time.Time) (*Credential, error) {
	c := r.Credential
	if c.Token == "" {
		return nil, exitcode.New(exitcode.ReauthRequired, "stored credential has no token — run `npmctl auth login`")
	}
	// A token supplied via --token or NPMCTL_TOKEN carries no expiry: we were
	// handed the secret, not its metadata. Trusting it and letting the API answer
	// 401 is correct — treating "unknown expiry" as "expired" would make every CI
	// and container invocation fail before it sent a single request.
	if c.Expires == "" {
		return c, nil
	}
	if c.Expired(now) {
		return nil, exitcode.New(exitcode.ReauthRequired,
			"token for %s expired at %s — run `npmctl auth login`", c.Identity, c.Expires)
	}
	// An ephemeral or env-supplied token is not ours to refresh or persist.
	if save == nil || m == nil || !c.NeedsRefresh(now) {
		return c, nil
	}
	tok, err := m.MintToken(ctx, DefaultExpiry)
	if err != nil {
		// A failed refresh is not fatal while the current token is still valid.
		return c, nil
	}
	refreshed := *c
	refreshed.Token = tok.Token
	refreshed.Expires = tok.Expires
	if err := save.Save(&refreshed); err != nil {
		return c, nil
	}
	return &refreshed, nil
}
