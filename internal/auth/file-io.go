// Low-level credential-file IO: cross-process locking, parsing, and the atomic
// replace that keeps a concurrent reader from seeing a half-written store.
package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"github.com/nnmduc/npmctl/internal/exitcode"
)

// lock serialises access across processes. Several npmctl invocations sharing a
// credential file is normal — a shell loop, a CI matrix — and an interleaved
// read-modify-write would otherwise drop one process's token.
func (s *FileStore) lock() (*flock.Flock, error) {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return nil, err
	}
	fl := flock.New(s.Path + ".lock")
	ctx, cancel := timeoutCtx(10 * time.Second)
	defer cancel()
	locked, err := fl.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, errors.New("timed out waiting for the credential file lock")
	}
	return fl, nil
}

func (s *FileStore) read() (*credentialFile, error) {
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return &credentialFile{Credentials: map[string]*Credential{}}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return &credentialFile{Credentials: map[string]*Credential{}}, nil
	}
	var cf credentialFile
	if err := json.Unmarshal(b, &cf); err != nil {
		// Never rewrite from a partial parse: doing so would silently discard
		// every credential the file still holds. Exit 9 sends the operator to
		// `auth login` with the path named.
		return nil, exitcode.Wrap(exitcode.ReauthRequired, err,
			"credential file %s is corrupt — inspect or delete it, then run `npmctl auth login`", s.Path)
	}
	if cf.Credentials == nil {
		cf.Credentials = map[string]*Credential{}
	}
	return &cf, nil
}

// writeAtomic replaces the file via a temp file and rename, so a crash or a
// concurrent reader never observes a half-written credential store.

// writeAtomic replaces the file via a temp file and rename, so a crash or a
// concurrent reader never observes a half-written credential store.
func (s *FileStore) writeAtomic(cf *credentialFile) error {
	b, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.Path)
	tmp, err := os.CreateTemp(dir, ".credentials-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.Path)
}

// Load returns the stored credential for the exact (profile, url, identity).
