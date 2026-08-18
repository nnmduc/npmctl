// Retention sweeping, entry parsing, and filename sanitisation.
package undo

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var unsafeChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// sanitize keeps a resource label usable as a filename. Domain names and
// profile names reach this from user input, so a path separator must not survive.

// sanitize keeps a resource label usable as a filename. Domain names and
// profile names reach this from user input, so a path separator must not survive.
func sanitize(s string) string {
	s = unsafeChars.ReplaceAllString(strings.TrimSpace(s), "-")
	s = strings.Trim(s, "-.")
	if s == "" {
		return "unknown"
	}
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// Append writes a pre-image and returns the stored entry, including the path.
// It is called BEFORE the mutating request, so a failed write still leaves
// evidence of the prior state.

func readEntry(path string) (*Entry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var e Entry
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("parse journal entry %s: %w", path, err)
	}
	return &e, nil
}

// Sweep deletes entries older than Retention across every profile and reports
// how many went. Called on each invocation: retention that depends on the
// operator remembering to prune is not retention.

// Sweep deletes entries older than Retention across every profile and reports
// how many went. Called on each invocation: retention that depends on the
// operator remembering to prune is not retention.
func (j *Journal) Sweep(now time.Time) (int, error) {
	profiles, err := os.ReadDir(j.Root)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	cutoff := now.Add(-Retention)
	removed := 0
	for _, p := range profiles {
		if !p.IsDir() {
			continue
		}
		dir := filepath.Join(j.Root, p.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				if os.Remove(filepath.Join(dir, f.Name())) == nil {
					removed++
				}
			}
		}
	}
	return removed, nil
}
