// Package certattempt tracks certificate issuance attempts per domain set.
//
// Let's Encrypt allows 5 duplicate certificates per week. The transport already
// refuses to retry mutating methods, which closes the automatic path — but nothing
// stops a human or an agent from re-running `cert create` after an ambiguous
// timeout. This journal closes that path by refusing the attempt in the binary,
// rather than only warning about it in documentation.
package certattempt

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

const (
	// MaxAttempts is the number of attempts permitted inside Window. Set below
	// Let's Encrypt's limit of 5 so a refusal happens before the quota is spent.
	MaxAttempts = 3
	// Window is the rolling period attempts are counted over, matching LE's week.
	Window = 7 * 24 * time.Hour
)

// Attempt is one recorded issuance request.
type Attempt struct {
	Time    string `json:"time"`
	Domains string `json:"domains"`
	Outcome string `json:"outcome"`
}

// Journal stores attempts per profile.
type Journal struct {
	Path string
}

// DefaultPath returns $XDG_STATE_HOME/npmctl/cert-attempts.json.
func DefaultPath() (string, error) {
	if x := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); x != "" {
		return filepath.Join(x, "npmctl", "cert-attempts.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "npmctl", "cert-attempts.json"), nil
}

// New opens the journal at its default location.
func New() (*Journal, error) {
	p, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return &Journal{Path: p}, nil
}

// Key normalises a domain set so ordering and case cannot disguise a repeat: the
// ACME authority counts the same names as the same certificate regardless of how
// they were typed.
func Key(profile string, domains []string) string {
	norm := make([]string, 0, len(domains))
	for _, d := range domains {
		norm = append(norm, strings.ToLower(strings.TrimSpace(d)))
	}
	sort.Strings(norm)
	return profile + "|" + strings.Join(norm, ",")
}

type journalFile struct {
	Attempts map[string][]Attempt `json:"attempts"`
}

func (j *Journal) read() (*journalFile, error) {
	b, err := os.ReadFile(j.Path)
	if errors.Is(err, os.ErrNotExist) || len(b) == 0 {
		return &journalFile{Attempts: map[string][]Attempt{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var jf journalFile
	if err := json.Unmarshal(b, &jf); err != nil {
		// A corrupt attempt journal must not block issuance outright, but it also
		// must not silently reset the count to zero. Fail closed with a clear message.
		return nil, exitcode.Wrap(exitcode.Refused, err,
			"certificate attempt journal %s is unreadable — inspect it, or pass --force to proceed", j.Path)
	}
	if jf.Attempts == nil {
		jf.Attempts = map[string][]Attempt{}
	}
	return &jf, nil
}

func (j *Journal) write(jf *journalFile) error {
	if err := os.MkdirAll(filepath.Dir(j.Path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(jf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(j.Path, b, 0o600)
}

// Recent returns attempts for a domain set inside the rolling window.
func (j *Journal) Recent(key string, now time.Time) ([]Attempt, error) {
	jf, err := j.read()
	if err != nil {
		return nil, err
	}
	cutoff := now.Add(-Window)
	var out []Attempt
	for _, a := range jf.Attempts[key] {
		if t, err := time.Parse(time.RFC3339Nano, a.Time); err == nil && t.After(cutoff) {
			out = append(out, a)
		}
	}
	return out, nil
}

// Check refuses a further attempt once the window is full.
func (j *Journal) Check(key string, now time.Time, force bool) error {
	recent, err := j.Recent(key, now)
	if err != nil {
		return err
	}
	if len(recent) < MaxAttempts || force {
		return nil
	}
	oldest := recent[0]
	next := "soon"
	if t, err := time.Parse(time.RFC3339Nano, oldest.Time); err == nil {
		next = t.Add(Window).UTC().Format(time.RFC3339)
	}
	return exitcode.New(exitcode.Refused,
		"refusing to request this certificate again: %d attempts in the last 7 days for the same domain set.\n"+
			"Let's Encrypt allows 5 duplicate certificates per week, and a further attempt risks the quota.\n"+
			"The window frees up after %s. Pass --force to override.",
		len(recent), next)
}

// Record appends an attempt and prunes entries outside the window.
func (j *Journal) Record(key, domains, outcome string, now time.Time) error {
	jf, err := j.read()
	if err != nil {
		return err
	}
	cutoff := now.Add(-Window)
	kept := []Attempt{}
	for _, a := range jf.Attempts[key] {
		if t, err := time.Parse(time.RFC3339Nano, a.Time); err == nil && t.After(cutoff) {
			kept = append(kept, a)
		}
	}
	kept = append(kept, Attempt{
		Time:    now.UTC().Format(time.RFC3339Nano),
		Domains: domains,
		Outcome: outcome,
	})
	jf.Attempts[key] = kept
	return j.write(jf)
}
