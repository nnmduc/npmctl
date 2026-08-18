package undo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const secretCredential = "sk_live_DNS_PROVIDER_TOKEN_abc123"

func newTestJournal(t *testing.T) *Journal {
	t.Helper()
	return &Journal{Root: filepath.Join(t.TempDir(), "undo")}
}

func certEntry() *Entry {
	pre, _ := json.Marshal(map[string]any{
		"id":       5,
		"provider": "letsencrypt",
		"meta": map[string]any{
			"dns_provider":             "cloudflare",
			"dns_provider_credentials": secretCredential,
		},
	})
	return &Entry{
		Profile: "prod", URL: "https://npm.example.com",
		Verb: "delete", Kind: "certificate", Resource: "certificate 5", TargetID: 5,
		Method: "DELETE", Path: "/nginx/certificates/5", PreImage: pre,
	}
}

// TestPreImageIsStoredRaw is deliberate and load-bearing: the output scrubber must
// NOT be applied to the journal. A pre-image containing "[redacted]" cannot be
// restored, which would defeat the journal's only purpose. The compensating
// controls are the 0600 mode and the retention sweep, both asserted below.
func TestPreImageIsStoredRaw(t *testing.T) {
	j := newTestJournal(t)
	path, err := j.Append(certEntry(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), secretCredential) {
		t.Fatalf("the pre-image was scrubbed, so a restore is impossible:\n%s", b)
	}
	if strings.Contains(string(b), "[redacted]") {
		t.Fatal("the journal must never contain a redaction placeholder")
	}
}

// TestJournalFileMode is one of the two controls that make raw storage acceptable.
func TestJournalFileMode(t *testing.T) {
	j := newTestJournal(t)
	path, err := j.Append(certEntry(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("journal entry mode = %o, want 600 — it holds plaintext secrets", perm)
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dir.Mode().Perm(); perm != 0o700 {
		t.Errorf("journal directory mode = %o, want 700", perm)
	}
}

// TestRetentionSweepDeletesOldEntries is the other control. Retention that relies
// on the operator remembering to prune is not retention, so the sweep runs on every
// invocation — here proven with backdated modification times.
func TestRetentionSweepDeletesOldEntries(t *testing.T) {
	j := newTestJournal(t)
	now := time.Now()

	fresh, err := j.Append(certEntry(), now)
	if err != nil {
		t.Fatal(err)
	}
	old, err := j.Append(certEntry(), now.Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	// Backdate past the retention window.
	stale := now.Add(-Retention - 24*time.Hour)
	if err := os.Chtimes(old, stale, stale); err != nil {
		t.Fatal(err)
	}

	removed, err := j.Sweep(now)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("swept %d entries, want 1", removed)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("an entry older than the retention window survived the sweep")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a fresh entry was deleted by the sweep")
	}
}

func TestSweepOnMissingRootIsNotAnError(t *testing.T) {
	j := &Journal{Root: filepath.Join(t.TempDir(), "does-not-exist")}
	if _, err := j.Sweep(time.Now()); err != nil {
		t.Errorf("sweeping a fresh install must not error: %v", err)
	}
}

func TestListReturnsNewestFirst(t *testing.T) {
	j := newTestJournal(t)
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		e := certEntry()
		e.TargetID = i
		if _, err := j.Append(e, base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := j.List("prod")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	for i := 1; i < len(entries); i++ {
		if entries[i-1].ID < entries[i].ID {
			t.Errorf("entries are not newest-first: %s before %s", entries[i-1].ID, entries[i].ID)
		}
	}
}

// TestEntriesAreProfileScoped keeps R10 true for the journal as well as the
// credential store.
func TestEntriesAreProfileScoped(t *testing.T) {
	j := newTestJournal(t)
	prod := certEntry()
	if _, err := j.Append(prod, time.Now()); err != nil {
		t.Fatal(err)
	}
	lab := certEntry()
	lab.Profile = "lab"
	if _, err := j.Append(lab, time.Now()); err != nil {
		t.Fatal(err)
	}

	entries, err := j.List("lab")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Profile != "lab" {
		t.Fatalf("lab listing returned %d entries from other profiles", len(entries))
	}
}

// TestResourceNameIsSanitised: domain names and profile names come from user input
// and must not be able to escape the journal directory.
func TestResourceNameIsSanitised(t *testing.T) {
	j := newTestJournal(t)
	e := certEntry()
	e.Profile = "../../etc"
	e.Kind = "../../../passwd"

	path, err := j.Append(e, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(path)
	root, _ := filepath.Abs(j.Root)
	if !strings.HasPrefix(abs, root) {
		t.Fatalf("journal entry escaped its root: %s", abs)
	}
}

// TestBodyFiltersToWritableFields proves a restore body satisfies both
// additionalProperties:false and minProperties:1.
func TestBodyFiltersToWritableFields(t *testing.T) {
	pre, _ := json.Marshal(map[string]any{
		"id":             12,
		"created_on":     "2026-01-01T00:00:00.000Z",
		"modified_on":    "2026-01-02T00:00:00.000Z",
		"owner_user_id":  1,
		"owner":          map[string]any{"id": 1},
		"certificate":    map[string]any{"id": 3},
		"domain_names":   []any{"app.example.com"},
		"forward_host":   "10.0.0.9",
		"forward_port":   8080,
		"forward_scheme": "http",
		"enabled":        true,
	})
	e := &Entry{Kind: "proxy-host", PreImage: pre}

	body, err := e.Body()
	if err != nil {
		t.Fatal(err)
	}
	for _, readOnly := range []string{"id", "created_on", "modified_on", "owner", "owner_user_id", "certificate"} {
		if _, present := body[readOnly]; present {
			t.Errorf("restore body includes the read-only field %q", readOnly)
		}
	}
	if body["forward_port"] != float64(8080) {
		t.Errorf("forward_port missing from the restore body: %v", body["forward_port"])
	}
	if len(body) == 0 {
		t.Error("restore body must be non-empty to satisfy minProperties:1")
	}
}

// TestBodyRefusesUnknownField: an entry written by a version whose schema no longer
// matches must be refused, not silently partially restored.
func TestBodyRefusesUnknownField(t *testing.T) {
	pre, _ := json.Marshal(map[string]any{
		"domain_names":        []any{"app.example.com"},
		"legacy_mystery_flag": true,
	})
	e := &Entry{Kind: "proxy-host", PreImage: pre}

	_, err := e.Body()
	if err == nil {
		t.Fatal("an unrecognised pre-image field must be refused")
	}
	var unknown *UnknownKeyError
	if !asUnknown(err, &unknown) {
		t.Fatalf("expected an UnknownKeyError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "legacy_mystery_flag") {
		t.Errorf("the error must name the offending field: %v", err)
	}
}

func TestBodyRefusesUnknownKind(t *testing.T) {
	e := &Entry{Kind: "no-such-resource", PreImage: json.RawMessage(`{}`)}
	if _, err := e.Body(); err == nil {
		t.Fatal("a kind with no restore rules must be refused")
	}
}

func asUnknown(err error, target **UnknownKeyError) bool {
	if e, ok := err.(*UnknownKeyError); ok {
		*target = e
		return true
	}
	return false
}
