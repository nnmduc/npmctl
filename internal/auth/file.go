package auth

import (
	"os"
	"path/filepath"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

// FileStore keeps credentials in a 0600 JSON file. It is the explicit fallback
// when no OS keyring is reachable — notably headless Linux, where there is no
// D-Bus session for Secret Service to talk to.
type FileStore struct {
	Path string
}

type credentialFile struct {
	Credentials map[string]*Credential `json:"credentials"`
}

// DefaultCredentialPath returns ~/.config/npmctl/credentials.json.

// DefaultCredentialPath returns ~/.config/npmctl/credentials.json.
func DefaultCredentialPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "npmctl", "credentials.json"), nil
}

// NewFileStore builds a file-backed store at the default path.

// NewFileStore builds a file-backed store at the default path.
func NewFileStore() (*FileStore, error) {
	p, err := DefaultCredentialPath()
	if err != nil {
		return nil, err
	}
	return &FileStore{Path: p}, nil
}

func (s *FileStore) Backend() string { return "file (" + s.Path + ")" }

// lock serialises access across processes. Several npmctl invocations sharing a
// credential file is normal — a shell loop, a CI matrix — and an interleaved
// read-modify-write would otherwise drop one process's token.

// Load returns the stored credential for the exact (profile, url, identity).
func (s *FileStore) Load(profile, url, identity string) (*Credential, error) {
	fl, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer fl.Unlock()
	cf, err := s.read()
	if err != nil {
		return nil, err
	}
	key := Key(profile, url, identity)
	if c, ok := cf.Credentials[key]; ok {
		return c, nil
	}
	return nil, staleOrMissing(cf, profile, url, identity)
}

// staleOrMissing distinguishes "never logged in" from "logged in against a
// different URL for this profile". The second case is R10: the credential exists
// but must not be reused, and saying so is far more useful than "not found".

// staleOrMissing distinguishes "never logged in" from "logged in against a
// different URL for this profile". The second case is R10: the credential exists
// but must not be reused, and saying so is far more useful than "not found".
func staleOrMissing(cf *credentialFile, profile, url, identity string) error {
	for _, c := range cf.Credentials {
		if c.Profile == profile && c.URL != trimSlash(url) {
			return exitcode.New(exitcode.ReauthRequired,
				"profile %q previously authenticated against %s but is now configured for %s — "+
					"stored credentials are not reused across URLs; run `npmctl auth login`",
				profile, c.URL, trimSlash(url))
		}
	}
	return &ErrNotFound{Key: Key(profile, url, identity)}
}

// Save writes a credential, preserving whichever entry expires later.

// Save writes a credential, preserving whichever entry expires later.
func (s *FileStore) Save(c *Credential) error {
	fl, err := s.lock()
	if err != nil {
		return err
	}
	defer fl.Unlock()
	cf, err := s.read()
	if err != nil {
		return err
	}
	key := c.Key()
	if existing, ok := cf.Credentials[key]; ok {
		// Two processes may both refresh. Keeping the later expiry means a
		// concurrent write cannot downgrade a freshly minted token.
		if ea, ok1 := existing.ExpiresAt(); ok1 {
			if na, ok2 := c.ExpiresAt(); ok2 && ea.After(na) {
				return nil
			}
		}
	}
	cf.Credentials[key] = c
	return s.writeAtomic(cf)
}

// Delete removes a credential.

// Delete removes a credential.
func (s *FileStore) Delete(profile, url, identity string) error {
	fl, err := s.lock()
	if err != nil {
		return err
	}
	defer fl.Unlock()
	cf, err := s.read()
	if err != nil {
		return err
	}
	delete(cf.Credentials, Key(profile, url, identity))
	return s.writeAtomic(cf)
}

// DeleteProfile removes every credential belonging to a profile, whatever URL
// it was minted against. `auth logout` needs this: a URL change would otherwise
// orphan the old entry in the file forever.

// DeleteProfile removes every credential belonging to a profile, whatever URL
// it was minted against. `auth logout` needs this: a URL change would otherwise
// orphan the old entry in the file forever.
func (s *FileStore) DeleteProfile(profile string) (int, error) {
	fl, err := s.lock()
	if err != nil {
		return 0, err
	}
	defer fl.Unlock()
	cf, err := s.read()
	if err != nil {
		return 0, err
	}
	n := 0
	for k, c := range cf.Credentials {
		if c.Profile == profile {
			delete(cf.Credentials, k)
			n++
		}
	}
	if n == 0 {
		return 0, nil
	}
	return n, s.writeAtomic(cf)
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
