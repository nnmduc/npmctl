// Package auth resolves and persists NPM credentials.
//
// npmctl stores a bounded long-lived TOKEN, not a password. POST /tokens has no
// expiry parameter, but GET /tokens?expiry= does and is unvalidated, so login
// mints a 30-day token and the password is discarded immediately. Storing the
// password would also be useless on a 2FA account, where re-login answers with a
// challenge rather than a token.
package auth

import (
	"fmt"
	"strings"
	"time"
)

// DefaultExpiry is the lifetime requested for a minted token: long enough that a
// human is not re-authenticating daily, short enough that a leaked RS256 token —
// which NPM cannot revoke — expires on its own.
const DefaultExpiry = "30d"

// RefreshWindow is how close to expiry a token is refreshed opportunistically.
const RefreshWindow = 72 * time.Hour

// Credential is one instance's stored secret material.
type Credential struct {
	Profile  string `json:"profile"`
	URL      string `json:"url"`
	Identity string `json:"identity"`
	Token    string `json:"token"`
	Expires  string `json:"expires"`

	// Password is empty unless the operator passed --store-password. It exists
	// for unattended fleets that accept the risk; it is never written by default
	// and never usable on a 2FA-enabled account.
	Password string `json:"password,omitempty"`
}

// Key identifies a credential by all three scoping dimensions.
//
// The URL is part of the key on purpose (R10): a credential minted for prod must
// never be sent to lab. Because a changed profile URL produces a different key,
// stale credentials cannot be replayed against a new host — the lookup simply
// misses instead of leaking.
func Key(profile, url, identity string) string {
	return fmt.Sprintf("%s|%s|%s", profile, strings.TrimRight(url, "/"), identity)
}

// Key returns the credential's own key.
func (c *Credential) Key() string { return Key(c.Profile, c.URL, c.Identity) }

// ExpiresAt parses the stored expiry. A credential with an unparseable expiry is
// treated as expired rather than as valid forever.
func (c *Credential) ExpiresAt() (time.Time, bool) {
	if c.Expires == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, c.Expires); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// Expired reports whether the token is past its expiry.
func (c *Credential) Expired(now time.Time) bool {
	at, ok := c.ExpiresAt()
	if !ok {
		return true
	}
	return now.After(at)
}

// NeedsRefresh reports whether the token is close enough to expiry to renew.
func (c *Credential) NeedsRefresh(now time.Time) bool {
	at, ok := c.ExpiresAt()
	if !ok {
		return true
	}
	return now.Add(RefreshWindow).After(at)
}

// Store persists credentials.
type Store interface {
	Load(profile, url, identity string) (*Credential, error)
	Save(c *Credential) error
	Delete(profile, url, identity string) error
	// Backend names the storage in use, so `auth status` can report it rather
	// than leaving the operator guessing where the secret went.
	Backend() string
}

// ErrNotFound is returned when no credential matches.
type ErrNotFound struct{ Key string }

func (e *ErrNotFound) Error() string { return "no stored credential for " + e.Key }

// IsNotFound reports whether err means "nothing stored".
func IsNotFound(err error) bool {
	_, ok := err.(*ErrNotFound)
	return ok
}
