package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// envWriterIndex marks a re-executed copy of this test acting as a writer process.
const (
	envWriterIndex = "NPMCTL_TEST_WRITER_INDEX"
	envWriterPath  = "NPMCTL_TEST_WRITER_PATH"
	writerCount    = 8
)

func testCredential(i int) *Credential {
	return &Credential{
		Profile:  "prod",
		URL:      "https://npm.example.com",
		Identity: fmt.Sprintf("user%d@example.com", i),
		Token:    fmt.Sprintf("token-%d", i),
		// Distinct expiries so the later-expiry merge rule is exercised too.
		Expires: time.Now().Add(time.Duration(i+1) * time.Hour).UTC().Format(time.RFC3339Nano),
	}
}

// TestFileStoreConcurrentProcesses runs eight real processes against one
// credential file.
//
// Goroutines would be a weaker test: the flock advisory lock and the atomic
// rename both exist to survive SEPARATE processes, which is the actual scenario —
// a shell loop, a CI matrix, or two terminals. So this test re-executes itself.
func TestFileStoreConcurrentProcesses(t *testing.T) {
	// Writer mode: this process is one of the eight children.
	if idxStr := os.Getenv(envWriterIndex); idxStr != "" {
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			t.Fatal(err)
		}
		store := &FileStore{Path: os.Getenv(envWriterPath)}
		if err := store.Save(testCredential(idx)); err != nil {
			t.Fatalf("writer %d failed: %v", idx, err)
		}
		return
	}

	path := filepath.Join(t.TempDir(), "credentials.json")
	var wg sync.WaitGroup
	errs := make(chan error, writerCount)

	for i := 0; i < writerCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestFileStoreConcurrentProcesses$")
			cmd.Env = append(os.Environ(),
				fmt.Sprintf("%s=%d", envWriterIndex, i),
				fmt.Sprintf("%s=%s", envWriterPath, path),
			)
			if out, err := cmd.CombinedOutput(); err != nil {
				errs <- fmt.Errorf("writer %d: %v\n%s", i, err, out)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	// The file must be valid JSON holding every writer's credential: a torn write
	// would fail to parse, and a lost update would drop an entry.
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("credential file unreadable after concurrent writes: %v", err)
	}
	var cf credentialFile
	if err := json.Unmarshal(b, &cf); err != nil {
		t.Fatalf("credential file is corrupt after %d concurrent writers: %v\n%s", writerCount, err, b)
	}
	if len(cf.Credentials) != writerCount {
		t.Errorf("got %d credentials, want %d — a concurrent write was lost", len(cf.Credentials), writerCount)
	}
	if info, err := os.Stat(path); err == nil {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("credential file mode = %o, want 600", perm)
		}
	}
}

// TestSaveKeepsLaterExpiry: two processes may both refresh, and the fresher token
// must win regardless of who writes last.
func TestSaveKeepsLaterExpiry(t *testing.T) {
	store := &FileStore{Path: filepath.Join(t.TempDir(), "credentials.json")}

	newer := testCredential(1)
	newer.Expires = time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	newer.Token = "fresh"
	if err := store.Save(newer); err != nil {
		t.Fatal(err)
	}

	older := testCredential(1)
	older.Expires = time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	older.Token = "stale"
	if err := store.Save(older); err != nil {
		t.Fatal(err)
	}

	got, err := store.Load(newer.Profile, newer.URL, newer.Identity)
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != "fresh" {
		t.Errorf("token = %q, want the one with the later expiry", got.Token)
	}
}

// TestCredentialsAreProfileScoped covers R10: a credential minted for one instance
// must never be handed to another.
func TestCredentialsAreProfileScoped(t *testing.T) {
	store := &FileStore{Path: filepath.Join(t.TempDir(), "credentials.json")}
	prod := &Credential{Profile: "prod", URL: "https://prod.example.com", Identity: "me@example.com", Token: "prod-token"}
	if err := store.Save(prod); err != nil {
		t.Fatal(err)
	}

	// A different profile must not find it.
	if _, err := store.Load("lab", "https://prod.example.com", "me@example.com"); err == nil {
		t.Error("a lab lookup must not return a prod credential")
	}
	// Nor must a different identity.
	if _, err := store.Load("prod", "https://prod.example.com", "other@example.com"); err == nil {
		t.Error("a different identity must not return this credential")
	}
}

// TestURLChangeInvalidatesStoredCredential is the concrete R10 failure: repointing
// a profile at a new host must not replay the old host's token, and the message
// must explain why rather than just saying "not found".
func TestURLChangeInvalidatesStoredCredential(t *testing.T) {
	store := &FileStore{Path: filepath.Join(t.TempDir(), "credentials.json")}
	if err := store.Save(&Credential{
		Profile: "prod", URL: "https://prod.example.com", Identity: "me@example.com", Token: "prod-token",
	}); err != nil {
		t.Fatal(err)
	}

	_, err := store.Load("prod", "https://attacker.example.com", "me@example.com")
	if err == nil {
		t.Fatal("a changed profile URL must invalidate the stored credential")
	}
	if IsNotFound(err) {
		t.Fatalf("expected an explanatory refusal, got a bare not-found: %v", err)
	}
}

// TestCorruptFileDoesNotDestroyCredentials: rewriting from a partial parse would
// silently discard every credential the file still holds.
func TestCorruptFileDoesNotDestroyCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, []byte(`{"credentials": {"broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &FileStore{Path: path}

	if _, err := store.Load("prod", "https://npm.example.com", "me@example.com"); err == nil {
		t.Fatal("a corrupt credential file must be reported, not ignored")
	}
	// The bytes must still be there for the operator to inspect.
	b, _ := os.ReadFile(path)
	if len(b) == 0 {
		t.Fatal("the corrupt file was truncated instead of preserved")
	}
}

func TestDeleteProfileRemovesEveryURL(t *testing.T) {
	store := &FileStore{Path: filepath.Join(t.TempDir(), "credentials.json")}
	_ = store.Save(&Credential{Profile: "prod", URL: "https://old.example.com", Identity: "me@x.com", Token: "a"})
	_ = store.Save(&Credential{Profile: "prod", URL: "https://new.example.com", Identity: "me@x.com", Token: "b"})
	_ = store.Save(&Credential{Profile: "lab", URL: "https://lab.example.com", Identity: "me@x.com", Token: "c"})

	n, err := store.DeleteProfile("prod")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("removed %d entries, want 2 — a stale URL entry would otherwise be orphaned", n)
	}
	if _, err := store.Load("lab", "https://lab.example.com", "me@x.com"); err != nil {
		t.Errorf("logging out of prod must not affect lab: %v", err)
	}
}
