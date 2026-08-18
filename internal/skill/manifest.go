package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// manifestName is where install records what it wrote.
const manifestName = ".npmctl-manifest.json"

// Manifest records a checksum per installed file.
//
// It exists to resolve a real tension: re-running install should pick up new content,
// but it must never silently discard an operator's local edits. With checksums, an
// unchanged file can be overwritten safely, and a modified one is reported instead.
type Manifest struct {
	Version   string            `json:"npmctl_version"`
	Checksums map[string]string `json:"checksums"`
}

// Sum returns the SHA-256 of content, hex-encoded.
func Sum(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

// LoadManifest reads the manifest from a skill directory, returning an empty one when
// absent — a first install has nothing recorded.
func LoadManifest(dir string) (*Manifest, error) {
	b, err := os.ReadFile(filepath.Join(dir, manifestName))
	if errors.Is(err, os.ErrNotExist) {
		return &Manifest{Checksums: map[string]string{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		// A corrupt manifest must not authorise overwriting: fall back to treating
		// every existing file as locally modified.
		return &Manifest{Checksums: map[string]string{}}, nil
	}
	if m.Checksums == nil {
		m.Checksums = map[string]string{}
	}
	return &m, nil
}

// Save writes the manifest.
func (m *Manifest) Save(dir string) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, manifestName), b, 0o644)
}

// Action is what install decided to do with one file.
type Action string

const (
	// ActionCreated: the file did not exist.
	ActionCreated Action = "created"
	// ActionUpdated: the file matched what we last wrote, so it was safe to replace.
	ActionUpdated Action = "updated"
	// ActionUnchanged: already identical to the new content.
	ActionUnchanged Action = "unchanged"
	// ActionPreserved: locally modified. Left alone and reported, never clobbered.
	ActionPreserved Action = "preserved (locally modified)"
)

// Result reports one file's outcome.
type Result struct {
	RelPath string `json:"path"`
	Action  Action `json:"action"`
}

// Plan decides what to do with one file, given what is on disk and what we last wrote.
func (m *Manifest) Plan(dir string, f File) (Action, error) {
	path := filepath.Join(dir, filepath.FromSlash(f.RelPath))
	existing, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ActionCreated, nil
	}
	if err != nil {
		return "", err
	}
	newSum := Sum(f.Content)
	existingSum := Sum(existing)
	if existingSum == newSum {
		return ActionUnchanged, nil
	}
	// The file differs from what we want to write. Whether that is safe depends on
	// whether it still matches what WE last wrote.
	if recorded, ok := m.Checksums[f.RelPath]; ok && recorded == existingSum {
		return ActionUpdated, nil
	}
	return ActionPreserved, nil
}
