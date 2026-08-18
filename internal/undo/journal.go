// Package undo records a pre-image of every object npmctl is about to mutate,
// and replays one back.
//
// SECURITY NOTE. Entries are stored RAW at mode 0600 — the output scrubber is
// deliberately NOT applied. A pre-image containing "[redacted]" cannot be
// restored, which defeats the journal's only purpose. The consequence is real: a
// certificate pre-image holds meta.dns_provider_credentials in plaintext. That
// matches the trust level of the credential file, which already holds a bearer
// token. Two obligations follow, both implemented here: a 30-day retention sweep
// runs on every invocation, and README documents the directory as sensitive.
package undo

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Retention is how long pre-images are kept. Bounded because they are plaintext
// secrets at rest, not because disk is scarce.
const Retention = 30 * 24 * time.Hour

// Entry is one captured pre-image.

// Entry is one captured pre-image.
type Entry struct {
	ID       string `json:"id"`
	Time     string `json:"time"`
	Profile  string `json:"profile"`
	URL      string `json:"url"`
	Verb     string `json:"verb"`
	Resource string `json:"resource"`
	Kind     string `json:"kind"`
	TargetID int    `json:"target_id"`

	// Method and Path record the call that was about to be made, for forensics.
	Method string `json:"method"`
	Path   string `json:"path"`

	// PreImage is the object exactly as the API returned it before the write.
	PreImage json.RawMessage `json:"pre_image"`

	// Note carries a caveat that survives into `undo show`, e.g. that ACME
	// revocation cannot be reversed.
	Note string `json:"note,omitempty"`
}

// Journal is a per-profile directory of pre-images.

// Journal is a per-profile directory of pre-images.
type Journal struct {
	Root string
}

// DefaultRoot returns $XDG_STATE_HOME/npmctl/undo, falling back to
// ~/.local/state/npmctl/undo.

// DefaultRoot returns $XDG_STATE_HOME/npmctl/undo, falling back to
// ~/.local/state/npmctl/undo.
func DefaultRoot() (string, error) {
	if x := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); x != "" {
		return filepath.Join(x, "npmctl", "undo"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "npmctl", "undo"), nil
}

// New builds a journal rooted at the default location.

// New builds a journal rooted at the default location.
func New() (*Journal, error) {
	root, err := DefaultRoot()
	if err != nil {
		return nil, err
	}
	return &Journal{Root: root}, nil
}

func (j *Journal) dir(profile string) string {
	return filepath.Join(j.Root, sanitize(profile))
}

// Append writes a pre-image and returns the stored entry, including the path.
// It is called BEFORE the mutating request, so a failed write still leaves
// evidence of the prior state.
func (j *Journal) Append(e *Entry, now time.Time) (string, error) {
	dir := j.dir(e.Profile)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	stamp := now.UTC().Format("20060102T150405.000Z")
	e.ID = fmt.Sprintf("%s-%s-%d", stamp, sanitize(e.Kind), e.TargetID)
	e.Time = now.UTC().Format(time.RFC3339Nano)
	path := filepath.Join(dir, e.ID+".json")

	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return "", err
	}
	// 0600 and O_EXCL: the entry is plaintext secret material and must never
	// silently overwrite another.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		return "", err
	}
	return path, nil
}

// List returns a profile's entries, newest first.

// List returns a profile's entries, newest first.
func (j *Journal) List(profile string) ([]*Entry, error) {
	dir := j.dir(profile)
	files, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Entry
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		e, err := readEntry(filepath.Join(dir, f.Name()))
		if err != nil {
			continue // a corrupt entry must not hide the rest
		}
		out = append(out, e)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID > out[b].ID })
	return out, nil
}

// Load returns one entry by ID, accepting a bare ID or a filename.

// Load returns one entry by ID, accepting a bare ID or a filename.
func (j *Journal) Load(profile, id string) (*Entry, error) {
	id = strings.TrimSuffix(filepath.Base(id), ".json")
	return readEntry(filepath.Join(j.dir(profile), id+".json"))
}
