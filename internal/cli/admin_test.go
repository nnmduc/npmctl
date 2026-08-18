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

// loadFixture reads a JSON fixture from testdata.
func loadFixture(t *testing.T, name string) any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// The DNS credential embedded in the audit-log fixture.
const auditDNSSecret = "SECRET_CF_TOKEN_zzz999"

// TestAuditLogRedactsDNSCredentials asserts redaction AT THIS ENDPOINT specifically.
//
// The P1 scrubber covers dns_provider_credentials generally, but this is the one endpoint
// in the read-only group with real agent value: it is in the skill's allowed-tools and it
// WILL be called. An inherited guarantee that nothing exercises is not a guarantee.
//
// The leak is real: NPM strips these credentials via omissions() when a certificate is
// created (certificate.js:225) but passes `meta: updatedCertificate` raw on renew (:916),
// so any DNS-01 renewal persists the provider token into audit_log.meta.
func TestAuditLogRedactsDNSCredentials(t *testing.T) {
	h := newHarness(t)
	h.route("GET", "/api/audit-log", http.StatusOK, loadFixture(t, "audit-log-with-dns-creds.json"))

	stdout, stderr, code := h.run("audit-log", "list")
	if code != exitcode.OK {
		t.Fatalf("want exit 0, got %d\n%s", code, stderr)
	}
	if strings.Contains(stdout, auditDNSSecret) {
		t.Errorf("audit-log leaked a DNS provider credential to stdout:\n%s", stdout)
	}
	if strings.Contains(stderr, auditDNSSecret) {
		t.Errorf("audit-log leaked a DNS provider credential to stderr:\n%s", stderr)
	}
	// The surrounding entry must still be usable, or redaction has cost us the feature.
	if !strings.Contains(stdout, "renewed") {
		t.Errorf("useful audit detail was lost: %s", stdout)
	}
}

// TestAuditLogRedactsAcrossEveryFormat: the leak must be closed in all renderers, not
// just the default one.
func TestAuditLogRedactsAcrossEveryFormat(t *testing.T) {
	for _, format := range []string{"json", "yaml", "table"} {
		t.Run(format, func(t *testing.T) {
			h := newHarness(t)
			h.route("GET", "/api/audit-log", http.StatusOK, loadFixture(t, "audit-log-with-dns-creds.json"))

			stdout, _, code := h.run("audit-log", "list", "-o", format)
			if code != exitcode.OK {
				t.Fatalf("want exit 0, got %d", code)
			}
			if strings.Contains(stdout, auditDNSSecret) {
				t.Errorf("-o %s leaked the DNS credential:\n%s", format, stdout)
			}
		})
	}
}

// TestAuditLogRedactsWithVerbose covers the transport log too.
func TestAuditLogRedactsWithVerbose(t *testing.T) {
	h := newHarness(t)
	h.route("GET", "/api/audit-log", http.StatusOK, loadFixture(t, "audit-log-with-dns-creds.json"))

	stdout, stderr, code := h.run("audit-log", "list", "-v")
	if code != exitcode.OK {
		t.Fatalf("want exit 0, got %d\n%s", code, stderr)
	}
	if strings.Contains(stdout+stderr, auditDNSSecret) {
		t.Errorf("-v leaked the DNS credential:\nstdout: %s\nstderr: %s", stdout, stderr)
	}
}

// TestAuditLogHasNoInventedPaginationFlags: internal/audit-log.js hardcodes .limit(100)
// and the validator whitelist is {expand, query}. Offering --limit/--offset would imply a
// capability the API does not have.
func TestAuditLogHasNoInventedPaginationFlags(t *testing.T) {
	root, _ := NewRootCommand()
	cmd, _, err := root.Find([]string{"audit-log", "list"})
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"limit", "offset", "page", "per-page"} {
		if cmd.Flags().Lookup(banned) != nil {
			t.Errorf("--%s does not exist in the NPM API and must not be offered", banned)
		}
	}
	for _, want := range []string{"expand", "query"} {
		if cmd.Flags().Lookup(want) == nil {
			t.Errorf("--%s is supported by the API and should be available", want)
		}
	}
}

// TestAuditLogSendsExpandAsOneCommaSeparatedValue guards the encoding bug that silently
// dropped every expansion: NPM checks `typeof req.query.expand === "string"`.
func TestAuditLogSendsExpandAsOneCommaSeparatedValue(t *testing.T) {
	h := newHarness(t)
	var rawQuery string
	h.routeFunc("GET", "/api/audit-log", func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]any{})
	})

	if _, _, code := h.run("audit-log", "list", "--expand", "user"); code != exitcode.OK {
		t.Fatalf("want exit 0, got %d", code)
	}
	if strings.Count(rawQuery, "expand=") != 1 {
		t.Errorf("expand must be a single parameter, got %q", rawQuery)
	}
}

// TestReadOnlyCommandsNeverConstructAGateOp is the structural guarantee that this phase
// ships no writes: a read command that reached the gate would be a bug in scoping.
func TestReadOnlyCommandsNeverConstructAGateOp(t *testing.T) {
	readOnly := [][]string{
		{"audit-log", "list"}, {"audit-log", "get"},
		{"report", "hosts"},
		{"settings", "list"}, {"settings", "get"},
		{"schema", "get"}, {"schema", "check"},
		{"health"},
		{"undo", "list"}, {"undo", "show"},
	}
	root, _ := NewRootCommand()
	for _, path := range readOnly {
		cmd, _, err := root.Find(path)
		if err != nil {
			t.Errorf("command %v is missing: %v", path, err)
			continue
		}
		// A read command must not expose write-gate flags of its own.
		for _, gateFlag := range []string{"cascade-ack", "allow-advanced-config"} {
			if cmd.Flags().Lookup(gateFlag) != nil {
				t.Errorf("%v is a read but declares --%s", path, gateFlag)
			}
		}
	}
}

// TestReadOnlyCommandsSendNoMutatingRequests proves it behaviourally as well.
func TestReadOnlyCommandsSendNoMutatingRequests(t *testing.T) {
	h := newHarness(t)
	h.allowWrites() // even with the gate open, a read must not mutate
	h.route("GET", "/api/audit-log", http.StatusOK, []any{})
	h.route("GET", "/api/reports/hosts", http.StatusOK, map[string]any{"proxy": 1, "redirection": 0, "stream": 0, "dead": 0})
	h.route("GET", "/api/settings", http.StatusOK, []any{map[string]any{"id": "default-site", "value": "congratulations"}})
	h.route("GET", "/api/settings/default-site", http.StatusOK, map[string]any{"id": "default-site", "value": "congratulations"})
	h.route("GET", "/api/", http.StatusOK, map[string]any{"status": "OK", "setup": true, "version": map[string]any{"major": 2, "minor": 15, "revision": 1}})

	for _, args := range [][]string{
		{"audit-log", "list"},
		{"report", "hosts"},
		{"settings", "list"},
		{"settings", "get", "default-site"},
		{"health"},
	} {
		if _, stderr, code := h.run(args...); code != exitcode.OK {
			t.Errorf("%v exited %d\n%s", args, code, stderr)
		}
	}
	if muts := h.mutations(); len(muts) != 0 {
		t.Fatalf("read-only commands sent mutating requests: %+v", muts)
	}
}

// TestSettingsHasNoWritePath: PUT /settings/{id} is deferred, so no command may exist for it.
func TestSettingsHasNoWritePath(t *testing.T) {
	root, _ := NewRootCommand()
	for _, banned := range []string{"set", "update", "create", "rm", "delete"} {
		if _, _, err := root.Find([]string{"settings", banned}); err == nil {
			if cmd, _, _ := root.Find([]string{"settings", banned}); cmd != nil && cmd.Name() == banned {
				t.Errorf("`settings %s` must not exist in v1", banned)
			}
		}
	}
}

// TestSchemaCheckDetectsDrift covers the exit-code contract: 1 on breaking drift.
func TestSchemaCheckDetectsDrift(t *testing.T) {
	h := newHarness(t)
	drifted := filepath.Join("..", "..", "testdata", "schema-drifted.json")

	stdout, stderr, code := h.run("schema", "check", "--from-file", drifted)
	if code != exitcode.Generic {
		t.Fatalf("want exit %d on drift, got %d\n%s", exitcode.Generic, code, stderr)
	}
	if !strings.Contains(stdout, "block_exploits") {
		t.Errorf("the report should name the drifted property:\n%s", stdout)
	}
}

// TestSchemaCheckPassesOnVolatileOnlyDifferences is R15 end to end: NPM rewrites
// info.version and servers[0].url per request, and those must never be reported.
func TestSchemaCheckPassesOnVolatileOnlyDifferences(t *testing.T) {
	h := newHarness(t)
	same := filepath.Join("..", "..", "testdata", "schema-volatile-only.json")

	stdout, stderr, code := h.run("schema", "check", "--from-file", same)
	if code != exitcode.OK {
		t.Fatalf("want exit 0 despite mutated info.version/servers[0].url, got %d\n%s\n%s", code, stdout, stderr)
	}
}

// TestSchemaCheckTreatsDeferredDriftAsInformational keeps unimplemented paths from
// failing the check.
func TestSchemaCheckTreatsDeferredDriftAsInformational(t *testing.T) {
	h := newHarness(t)
	deferred := filepath.Join("..", "..", "testdata", "schema-drifted-deferred.json")

	stdout, _, code := h.run("schema", "check", "--from-file", deferred)
	if code != exitcode.OK {
		t.Fatalf("drift confined to deferred paths must exit 0, got %d\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "deferred") {
		t.Errorf("the report should mark the finding as deferred:\n%s", stdout)
	}
}

// TestAuditLogGetRejectsNonNumericID keeps a malformed id out of the request path.
func TestAuditLogGetRejectsNonNumericID(t *testing.T) {
	h := newHarness(t)
	_, _, code := h.run("audit-log", "get", "not-a-number")
	if code != exitcode.Usage {
		t.Fatalf("want exit %d, got %d", exitcode.Usage, code)
	}
}
