package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

// writeCommands enumerates every mutating command in the binary. The gate is
// asserted across all of them table-driven, so a new write command that forgets
// to route through Guard fails here rather than in production.
var writeCommands = [][]string{
	{"host", "create", "--domain", "new.example.com", "--forward-host", "10.0.0.5", "--forward-port", "80"},
	{"host", "update", "12", "--forward-port", "9090"},
	{"host", "rm", "12"},
	{"host", "enable", "12"},
	{"host", "disable", "12"},
}

// seedHost registers the reads every write command performs to resolve and
// verify its target.
func seedHost(h *harness) {
	h.route("GET", "/api/nginx/proxy-hosts/12", http.StatusOK, sampleHost())
	h.route("GET", "/api/nginx/proxy-hosts", http.StatusOK, []any{sampleHost()})
}

func TestWritesRefusedWithoutBothFactors(t *testing.T) {
	cases := []struct {
		name       string
		allowWrite bool
		yes        bool
	}{
		{"neither factor", false, false},
		{"only NPMCTL_ALLOW_WRITE", true, false},
		{"only --yes", false, true},
	}
	for _, tc := range cases {
		for _, args := range writeCommands {
			t.Run(tc.name+"/"+strings.Join(args[:2], "-"), func(t *testing.T) {
				h := newHarness(t)
				seedHost(h)
				if tc.allowWrite {
					h.allowWrites()
				}
				full := args
				if tc.yes {
					full = append(append([]string{}, args...), "--yes")
				}
				_, stderr, code := h.run(full...)

				if code != exitcode.Refused {
					t.Fatalf("want exit %d (refused), got %d\nstderr: %s", exitcode.Refused, code, stderr)
				}
				if muts := h.mutations(); len(muts) != 0 {
					t.Fatalf("a refused write still sent mutating requests: %+v", muts)
				}
			})
		}
	}
}

func TestWriteSucceedsWithBothFactors(t *testing.T) {
	h := newHarness(t)
	seedHost(h)
	h.allowWrites()
	updated := sampleHost()
	updated["forward_port"] = 9090
	h.route("PUT", "/api/nginx/proxy-hosts/12", http.StatusOK, updated)

	stdout, stderr, code := h.run("host", "update", "12", "--forward-port", "9090", "--yes")
	if code != exitcode.OK {
		t.Fatalf("want exit 0, got %d\nstderr: %s", code, stderr)
	}
	if h.countRequests("PUT", "/api/nginx/proxy-hosts/12") != 1 {
		t.Fatalf("expected exactly one PUT, got %d", h.countRequests("PUT", "/api/nginx/proxy-hosts/12"))
	}
	if got := decodeJSON(t, stdout)["forward_port"]; got != float64(9090) {
		t.Fatalf("forward_port = %v, want 9090", got)
	}
}

// TestDryRunIssuesNoMutatingRequest proves the invariant by METHOD: reads are
// permitted (a preview must be able to name its target), mutations are not.
func TestDryRunIssuesNoMutatingRequest(t *testing.T) {
	for _, args := range writeCommands {
		t.Run(strings.Join(args[:2], "-"), func(t *testing.T) {
			h := newHarness(t)
			seedHost(h)
			h.allowWrites()
			// Register mutating routes so a leaked request would succeed loudly
			// rather than 404 and look like a refusal.
			h.route("PUT", "/api/nginx/proxy-hosts/12", http.StatusOK, sampleHost())
			h.route("POST", "/api/nginx/proxy-hosts", http.StatusCreated, sampleHost())
			h.route("DELETE", "/api/nginx/proxy-hosts/12", http.StatusOK, nil)
			h.route("POST", "/api/nginx/proxy-hosts/12/enable", http.StatusOK, nil)
			h.route("POST", "/api/nginx/proxy-hosts/12/disable", http.StatusOK, nil)

			full := append(append([]string{}, args...), "--dry-run", "--yes")
			stdout, stderr, code := h.run(full...)

			if code != exitcode.OK {
				t.Fatalf("dry run should exit 0, got %d\nstderr: %s", code, stderr)
			}
			if muts := h.mutations(); len(muts) != 0 {
				t.Fatalf("--dry-run sent mutating request(s): %+v", muts)
			}
			if got := decodeJSON(t, stdout)["dry_run"]; got != true {
				t.Fatalf(`stdout lacks "dry_run": true; got %v`, got)
			}
			if !strings.Contains(stderr, "DRY RUN") {
				t.Fatalf("stderr lacks the DRY RUN banner: %s", stderr)
			}
		})
	}
}

// TestCompareAndSwapAbortsOnConcurrentChange covers R8: NPM has no ETag, so the
// gate re-reads immediately before writing and refuses if modified_on moved.
func TestCompareAndSwapAbortsOnConcurrentChange(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	h.route("GET", "/api/nginx/proxy-hosts", http.StatusOK, []any{sampleHost()})
	h.route("PUT", "/api/nginx/proxy-hosts/12", http.StatusOK, sampleHost())

	// A host update reads the target three times: once to resolve the reference,
	// once for the gate's preview, and once more immediately before writing. The
	// concurrent edit is simulated on that third read only — that is precisely the
	// window compare-and-swap exists to close.
	calls := 0
	h.routeFunc("GET", "/api/nginx/proxy-hosts/12", func(w http.ResponseWriter, _ *http.Request) {
		calls++
		host := sampleHost()
		if calls >= 3 {
			host["modified_on"] = "2026-06-06T00:00:00.000Z"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(host)
	})

	_, stderr, code := h.run("host", "update", "12", "--forward-port", "9090", "--yes")
	if code != exitcode.Refused {
		t.Fatalf("want exit %d on a concurrent change, got %d\nstderr: %s", exitcode.Refused, code, stderr)
	}
	if muts := h.mutations(); len(muts) != 0 {
		t.Fatalf("wrote despite a detected concurrent change: %+v", muts)
	}
	if !strings.Contains(stderr, "changed since it was previewed") {
		t.Fatalf("stderr does not explain the abort: %s", stderr)
	}
}

// TestNginxUnhealthyExitsEight covers R2: NPM answers 200 for a write whose
// nginx reload failed, recording the reason in meta.
func TestNginxUnhealthyExitsEight(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	h.route("GET", "/api/nginx/proxy-hosts", http.StatusOK, []any{sampleHost()})
	h.route("PUT", "/api/nginx/proxy-hosts/12", http.StatusOK, sampleHost())

	const nginxErr = "nginx: [emerg] invalid host in upstream"
	calls := 0
	h.routeFunc("GET", "/api/nginx/proxy-hosts/12", func(w http.ResponseWriter, _ *http.Request) {
		calls++
		host := sampleHost()
		// Reads are: resolve, preview, compare-and-swap, then the post-write
		// verification. Only the last must report the failed reload, so the CAS
		// check still sees an unchanged modified_on and lets the write proceed.
		if calls >= 4 {
			host["meta"] = map[string]any{"nginx_online": false, "nginx_err": nginxErr}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(host)
	})

	_, stderr, code := h.run("host", "update", "12", "--forward-port", "9090", "--yes")
	if code != exitcode.NginxUnhealthy {
		t.Fatalf("want exit %d when nginx is offline, got %d\nstderr: %s", exitcode.NginxUnhealthy, code, stderr)
	}
	if !strings.Contains(stderr, nginxErr) {
		t.Fatalf("nginx_err was not printed verbatim:\n%s", stderr)
	}
	if !strings.Contains(stderr, "undo list") {
		t.Fatalf("exit 8 must point at the recovery path:\n%s", stderr)
	}
}

// TestPreImageCapturedBeforeEveryMutation asserts the journal entry exists and
// that it was written before the mutating call.
func TestPreImageCapturedBeforeEveryMutation(t *testing.T) {
	h := newHarness(t)
	seedHost(h)
	h.allowWrites()

	var journalExistedAtWriteTime bool
	h.routeFunc("PUT", "/api/nginx/proxy-hosts/12", func(w http.ResponseWriter, _ *http.Request) {
		journalExistedAtWriteTime = len(journalFiles(t, h)) > 0
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sampleHost())
	})

	_, stderr, code := h.run("host", "update", "12", "--forward-port", "9090", "--yes")
	if code != exitcode.OK {
		t.Fatalf("want exit 0, got %d\nstderr: %s", code, stderr)
	}
	if !journalExistedAtWriteTime {
		t.Fatal("pre-image was not on disk when the mutating request was made")
	}
	files := journalFiles(t, h)
	if len(files) != 1 {
		t.Fatalf("want exactly 1 journal entry, got %d", len(files))
	}
	if !strings.Contains(stderr, "undo pre-image:") {
		t.Fatalf("the journal path was not reported on stderr: %s", stderr)
	}
}

// journalFiles lists journal entries for the default profile.
func journalFiles(t *testing.T, h *harness) []string {
	t.Helper()
	pattern := filepath.Join(h.home, ".state", "npmctl", "undo", "*", "*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestAdvancedConfigRequiresItsOwnFlag(t *testing.T) {
	h := newHarness(t)
	seedHost(h)
	h.allowWrites()
	h.route("PUT", "/api/nginx/proxy-hosts/12", http.StatusOK, sampleHost())

	_, stderr, code := h.run("host", "update", "12", "--advanced-config", "location /x { alias /data; }", "--yes")
	if code != exitcode.Refused {
		t.Fatalf("want exit %d without --allow-advanced-config, got %d\nstderr: %s", exitcode.Refused, code, stderr)
	}
	if !strings.Contains(stderr, "--allow-advanced-config") {
		t.Fatalf("refusal does not name the required flag: %s", stderr)
	}
	if muts := h.mutations(); len(muts) != 0 {
		t.Fatalf("advanced_config was written without its gate: %+v", muts)
	}
}

// TestAdvancedConfigRefusesNonTTY covers the Privileged-tier rule that cannot be
// satisfied by an environment variable: no terminal, no write.
func TestAdvancedConfigRefusesNonTTY(t *testing.T) {
	h := newHarness(t)
	seedHost(h)
	h.allowWrites()
	h.route("PUT", "/api/nginx/proxy-hosts/12", http.StatusOK, sampleHost())

	_, stderr, code := h.run("host", "update", "12",
		"--advanced-config", "location /x { alias /data; }", "--allow-advanced-config", "--yes")
	if code != exitcode.Refused {
		t.Fatalf("want exit %d with no TTY, got %d\nstderr: %s", exitcode.Refused, code, stderr)
	}
	if !strings.Contains(stderr, "interactive terminal") {
		t.Fatalf("refusal does not cite the terminal requirement: %s", stderr)
	}
	if muts := h.mutations(); len(muts) != 0 {
		t.Fatalf("privileged write proceeded without a TTY: %+v", muts)
	}
}

func TestNotFoundExitsFive(t *testing.T) {
	h := newHarness(t)
	h.route("GET", "/api/nginx/proxy-hosts/99", http.StatusNotFound,
		map[string]any{"error": map[string]any{"code": 404, "message": "not found"}})

	_, _, code := h.run("host", "get", "99")
	if code != exitcode.NotFound {
		t.Fatalf("want exit %d, got %d", exitcode.NotFound, code)
	}
}

func TestUnknownFlagExitsTwo(t *testing.T) {
	h := newHarness(t)
	_, _, code := h.run("host", "list", "--nonsense")
	if code != exitcode.Usage {
		t.Fatalf("want exit %d for a usage error, got %d", exitcode.Usage, code)
	}
}

func TestMissingCredentialExitsNine(t *testing.T) {
	h := newHarness(t)
	// Remove the env token so the resolution chain finds nothing at all.
	t.Setenv("NPMCTL_TOKEN", "")
	os.Unsetenv("NPMCTL_TOKEN")

	_, stderr, code := h.run("host", "list")
	if code != exitcode.ReauthRequired {
		t.Fatalf("want exit %d with no credential, got %d\nstderr: %s", exitcode.ReauthRequired, code, stderr)
	}
}
