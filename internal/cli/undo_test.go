package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

// asInteractive makes the Privileged tier's terminal check pass for one test, so
// the typed confirmation is exercised rather than skipped. The refusal path is
// covered separately by TestAdvancedConfigRefusesNonTTY.
func asInteractive(t *testing.T) {
	t.Helper()
	prev := interactiveCheck
	interactiveCheck = func(io.Writer) bool { return true }
	t.Cleanup(func() { interactiveCheck = prev })
}

// captureUpdates records every PUT body the fake instance receives.
func captureUpdates(h *harness, respond func() map[string]any) *[]map[string]any {
	bodies := &[]map[string]any{}
	h.routeFunc("PUT", "/api/nginx/proxy-hosts/12", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		*bodies = append(*bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(respond())
	})
	return bodies
}

// firstJournalEntryID returns the id of the single captured pre-image.
func firstJournalEntryID(t *testing.T, h *harness) string {
	t.Helper()
	files := journalFiles(t, h)
	if len(files) == 0 {
		t.Fatal("no journal entry was captured")
	}
	name := files[0]
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return strings.TrimSuffix(name, ".json")
}

// TestUndoApplyRoundTrips is the recovery guarantee: a host update, then an undo,
// returns the object to the state the pre-image recorded.
func TestUndoApplyRoundTrips(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	seedHost(h)
	bodies := captureUpdates(h, sampleHost)

	// Change the port away from the fixture's 8080.
	if _, stderr, code := h.run("host", "update", "12", "--forward-port", "9090", "--yes"); code != exitcode.OK {
		t.Fatalf("update failed with %d: %s", code, stderr)
	}
	if len(*bodies) != 1 || (*bodies)[0]["forward_port"] != float64(9090) {
		t.Fatalf("expected the update to send port 9090, got %v", *bodies)
	}

	entry := firstJournalEntryID(t, h)
	asInteractive(t)
	h.stdin = "proxy-host\n" // the typed confirmation the Privileged tier demands

	_, stderr, code := h.run("undo", "apply", entry, "--yes")
	if code != exitcode.OK {
		t.Fatalf("undo apply failed with %d: %s", code, stderr)
	}
	if len(*bodies) != 2 {
		t.Fatalf("expected a second PUT from the restore, got %d", len(*bodies))
	}

	restore := (*bodies)[1]
	// The pre-image held the original port, so the restore must send it back.
	if restore["forward_port"] != float64(8080) {
		t.Errorf("restore sent forward_port %v, want the pre-image value 8080", restore["forward_port"])
	}
	// A restore must not carry read-only fields, which additionalProperties:false
	// would reject.
	for _, readOnly := range []string{"id", "created_on", "modified_on", "owner", "owner_user_id"} {
		if _, present := restore[readOnly]; present {
			t.Errorf("restore body includes read-only field %q", readOnly)
		}
	}
}

// TestUndoApplyIsGated proves the restore receives no exemption from the write gate.
func TestUndoApplyIsGated(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	seedHost(h)
	captureUpdates(h, sampleHost)

	if _, _, code := h.run("host", "update", "12", "--forward-port", "9090", "--yes"); code != exitcode.OK {
		t.Fatalf("setup update failed: %d", code)
	}
	entry := firstJournalEntryID(t, h)

	// No --yes: refused, exactly like any other mutation.
	t.Setenv("NPMCTL_ALLOW_WRITE", "1")
	_, stderr, code := h.run("undo", "apply", entry)
	if code != exitcode.Refused {
		t.Fatalf("undo apply without --yes should exit %d, got %d\n%s", exitcode.Refused, code, stderr)
	}

	// No NPMCTL_ALLOW_WRITE: also refused.
	t.Setenv("NPMCTL_ALLOW_WRITE", "")
	_, stderr, code = h.run("undo", "apply", entry, "--yes")
	if code != exitcode.Refused {
		t.Fatalf("undo apply without the env factor should exit %d, got %d\n%s", exitcode.Refused, code, stderr)
	}
}

// TestUndoApplyCapturesItsOwnPreImage: restoring is itself undoable.
func TestUndoApplyCapturesItsOwnPreImage(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	seedHost(h)
	captureUpdates(h, sampleHost)

	if _, _, code := h.run("host", "update", "12", "--forward-port", "9090", "--yes"); code != exitcode.OK {
		t.Fatal("setup update failed")
	}
	entry := firstJournalEntryID(t, h)

	asInteractive(t)
	h.stdin = "proxy-host\n"
	if _, stderr, code := h.run("undo", "apply", entry, "--yes"); code != exitcode.OK {
		t.Fatalf("undo apply failed with %d: %s", code, stderr)
	}
	if got := len(journalFiles(t, h)); got != 2 {
		t.Errorf("journal holds %d entries, want 2 — the restore must capture its own pre-image", got)
	}
}

// TestUndoApplyRefusesNonTTY: the restore is Privileged, so an unattended process
// cannot replay a pre-image it never read.
func TestUndoApplyRefusesNonTTY(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	seedHost(h)
	captureUpdates(h, sampleHost)

	if _, _, code := h.run("host", "update", "12", "--forward-port", "9090", "--yes"); code != exitcode.OK {
		t.Fatal("setup update failed")
	}
	entry := firstJournalEntryID(t, h)

	_, stderr, code := h.run("undo", "apply", entry, "--yes")
	if code != exitcode.Refused {
		t.Fatalf("want exit %d with no terminal, got %d\n%s", exitcode.Refused, code, stderr)
	}
	if !strings.Contains(stderr, "interactive terminal") {
		t.Errorf("refusal should cite the terminal requirement: %s", stderr)
	}
}

// TestUndoApplyOnDeletedResourceExitsFive: an in-place restore is impossible, and
// recreating would mint a different ID, so it refuses and hands over the command.
func TestUndoApplyOnDeletedResourceExitsFive(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	seedHost(h)
	captureUpdates(h, sampleHost)

	if _, _, code := h.run("host", "update", "12", "--forward-port", "9090", "--yes"); code != exitcode.OK {
		t.Fatal("setup update failed")
	}
	entry := firstJournalEntryID(t, h)

	// The host is now gone.
	h.route("GET", "/api/nginx/proxy-hosts/12", http.StatusNotFound,
		map[string]any{"error": map[string]any{"code": 404, "message": "not found"}})

	asInteractive(t)
	h.stdin = "proxy-host\n"
	_, stderr, code := h.run("undo", "apply", entry, "--yes")
	if code != exitcode.NotFound {
		t.Fatalf("want exit %d for a deleted resource, got %d\n%s", exitcode.NotFound, code, stderr)
	}
	if !strings.Contains(stderr, "host create") {
		t.Errorf("the refusal should name the command that would recreate it: %s", stderr)
	}
}

// TestUndoApplyRefusesCrossProfile keeps R10 true for the journal.
func TestUndoApplyRefusesCrossProfile(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	seedHost(h)
	captureUpdates(h, sampleHost)

	if _, _, code := h.run("host", "update", "12", "--forward-port", "9090", "--yes"); code != exitcode.OK {
		t.Fatal("setup update failed")
	}
	entry := firstJournalEntryID(t, h)

	// Same profile name, different URL: the entry must not be replayed.
	_, stderr, code := h.run("--url", "https://elsewhere.example.com", "undo", "apply", entry, "--yes")
	if code != exitcode.Refused {
		t.Fatalf("want exit %d when the profile URL changed, got %d\n%s", exitcode.Refused, code, stderr)
	}
}

// TestUndoShowRedactsButFileDoesNot is the two-sided guarantee: display is scrubbed,
// the file on disk is not, so the restore still works.
func TestUndoShowRedactsButFileDoesNot(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()

	const secret = "sk_live_SUPER_SECRET_dns_token"
	hostWithSecret := sampleHost()
	hostWithSecret["meta"] = map[string]any{
		"nginx_online":             true,
		"dns_provider_credentials": secret,
	}
	h.route("GET", "/api/nginx/proxy-hosts", http.StatusOK, []any{hostWithSecret})
	h.route("GET", "/api/nginx/proxy-hosts/12", http.StatusOK, hostWithSecret)
	h.route("PUT", "/api/nginx/proxy-hosts/12", http.StatusOK, hostWithSecret)

	if _, stderr, code := h.run("host", "update", "12", "--forward-port", "9090", "--yes"); code != exitcode.OK {
		t.Fatalf("setup update failed with %d: %s", code, stderr)
	}
	entry := firstJournalEntryID(t, h)

	stdout, _, code := h.run("undo", "show", entry)
	if code != exitcode.OK {
		t.Fatalf("undo show failed with %d", code)
	}
	if strings.Contains(stdout, secret) {
		t.Errorf("undo show leaked a secret:\n%s", stdout)
	}

	// The stored file must still hold the real value, or a restore is worthless.
	files := journalFiles(t, h)
	raw, err := readFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, secret) {
		t.Error("the journal file was scrubbed, which would make the restore useless")
	}
}

func TestUndoListShowsCapturedEntries(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	seedHost(h)
	captureUpdates(h, sampleHost)

	if _, _, code := h.run("host", "update", "12", "--forward-port", "9090", "--yes"); code != exitcode.OK {
		t.Fatal("setup update failed")
	}
	stdout, _, code := h.run("undo", "list")
	if code != exitcode.OK {
		t.Fatalf("undo list failed with %d", code)
	}
	if !strings.Contains(stdout, "proxy-host") {
		t.Errorf("undo list did not report the captured entry:\n%s", stdout)
	}
}
