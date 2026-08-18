package certattempt

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

func newTestJournal(t *testing.T) *Journal {
	t.Helper()
	return &Journal{Path: filepath.Join(t.TempDir(), "cert-attempts.json")}
}

var domains = []string{"app.example.com", "www.app.example.com"}

// TestRefusesFourthAttemptInWindow is R6's enforcement point. Let's Encrypt allows 5
// duplicate certificates per week; refusing at 3 leaves headroom to recover manually
// instead of discovering the limit by hitting it.
func TestRefusesFourthAttemptInWindow(t *testing.T) {
	j := newTestJournal(t)
	now := time.Now()
	key := Key("prod", domains)

	for i := 0; i < MaxAttempts; i++ {
		if err := j.Check(key, now, false); err != nil {
			t.Fatalf("attempt %d should be allowed: %v", i+1, err)
		}
		if err := j.Record(key, "domains", "requested", now); err != nil {
			t.Fatal(err)
		}
	}

	err := j.Check(key, now, false)
	if err == nil {
		t.Fatalf("attempt %d must be refused", MaxAttempts+1)
	}
	if got := exitcode.Of(err); got != exitcode.Refused {
		t.Errorf("exit code = %d, want %d (refused)", got, exitcode.Refused)
	}
	// The refusal has to tell the operator when it lifts, or they will simply retry.
	if !contains(err.Error(), "after") || !contains(err.Error(), "--force") {
		t.Errorf("refusal must say when the window frees and how to override:\n%v", err)
	}
}

// TestForceOverridesCooldown keeps the escape hatch available: this is a safety
// guard, not a policy the operator cannot overrule.
func TestForceOverridesCooldown(t *testing.T) {
	j := newTestJournal(t)
	now := time.Now()
	key := Key("prod", domains)
	for i := 0; i < MaxAttempts; i++ {
		_ = j.Record(key, "domains", "requested", now)
	}
	if err := j.Check(key, now, true); err != nil {
		t.Errorf("--force must override the cooldown: %v", err)
	}
}

// TestAttemptsExpireAfterWindow: the limit is a rolling week, not a permanent cap.
func TestAttemptsExpireAfterWindow(t *testing.T) {
	j := newTestJournal(t)
	old := time.Now().Add(-Window - time.Hour)
	key := Key("prod", domains)
	for i := 0; i < MaxAttempts; i++ {
		_ = j.Record(key, "domains", "requested", old)
	}
	if err := j.Check(key, time.Now(), false); err != nil {
		t.Errorf("attempts outside the window must not count: %v", err)
	}
}

// TestKeyIgnoresOrderAndCase matters because the ACME authority counts the same
// names as the same certificate however they were typed — so the cooldown must too.
func TestKeyIgnoresOrderAndCase(t *testing.T) {
	a := Key("prod", []string{"a.example.com", "b.example.com"})
	b := Key("prod", []string{"B.example.com", " a.example.com "})
	if a != b {
		t.Errorf("domain sets differing only in order/case must share a key:\n%s\n%s", a, b)
	}
}

// TestKeyIsProfileScoped stops a lab attempt from consuming prod's budget.
func TestKeyIsProfileScoped(t *testing.T) {
	if Key("prod", domains) == Key("lab", domains) {
		t.Error("attempt keys must be scoped per profile")
	}
}

// TestDifferentDomainSetsAreIndependent: the ACME limit is per certificate, so an
// unrelated domain set must not be blocked.
func TestDifferentDomainSetsAreIndependent(t *testing.T) {
	j := newTestJournal(t)
	now := time.Now()
	busy := Key("prod", domains)
	for i := 0; i < MaxAttempts; i++ {
		_ = j.Record(busy, "domains", "requested", now)
	}
	other := Key("prod", []string{"other.example.com"})
	if err := j.Check(other, now, false); err != nil {
		t.Errorf("an unrelated domain set must not be blocked: %v", err)
	}
}

// TestRecordPrunesOldEntries keeps the file from growing without bound.
func TestRecordPrunesOldEntries(t *testing.T) {
	j := newTestJournal(t)
	key := Key("prod", domains)
	_ = j.Record(key, "domains", "requested", time.Now().Add(-Window-time.Hour))
	_ = j.Record(key, "domains", "requested", time.Now())

	recent, err := j.Recent(key, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 {
		t.Errorf("expected the stale entry to be pruned, got %d entries", len(recent))
	}
}

func contains(h, n string) bool {
	return len(h) >= len(n) && (h == n || indexOf(h, n) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
