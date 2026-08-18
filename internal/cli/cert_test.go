package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

func sampleCert(provider string) map[string]any {
	return map[string]any{
		"id":           7,
		"created_on":   "2026-01-01T00:00:00.000Z",
		"modified_on":  "2026-01-02T00:00:00.000Z",
		"provider":     provider,
		"nice_name":    "example.com",
		"domain_names": []string{"example.com"},
		"expires_on":   "2027-01-01 00:00:00",
		"meta":         map[string]any{"dns_challenge": false},
	}
}

// seedCert registers the certificate plus the four host collections the dependency
// scan reads.
func seedCert(h *harness, provider string, referencing bool) {
	h.route("GET", "/api/nginx/certificates", http.StatusOK, []any{sampleCert(provider)})
	h.route("GET", "/api/nginx/certificates/7", http.StatusOK, sampleCert(provider))

	host := sampleHost()
	if referencing {
		host["certificate_id"] = 7
	} else {
		host["certificate_id"] = 0
	}
	h.route("GET", "/api/nginx/proxy-hosts", http.StatusOK, []any{host})
	h.route("GET", "/api/nginx/redirection-hosts", http.StatusOK, []any{})
	h.route("GET", "/api/nginx/dead-hosts", http.StatusOK, []any{})
	h.route("GET", "/api/nginx/streams", http.StatusOK, []any{})
	h.route("DELETE", "/api/nginx/certificates/7", http.StatusOK, nil)
}

// TestCertRemoveWarnsAboutIrreversibleRevocation covers R1. Deleting a letsencrypt
// certificate calls revokeLetsEncryptSsl(): the material is gone, and no journal entry
// can bring it back.
func TestCertRemoveWarnsAboutIrreversibleRevocation(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	seedCert(h, "letsencrypt", false)

	_, stderr, code := h.run("cert", "rm", "7", "--yes")
	if code != exitcode.OK {
		t.Fatalf("want exit 0, got %d\n%s", code, stderr)
	}
	low := strings.ToLower(stderr)
	if !strings.Contains(low, "revoke") {
		t.Errorf("the warning must say the certificate is revoked:\n%s", stderr)
	}
	if !strings.Contains(low, "cannot be undone") && !strings.Contains(low, "irreversible") {
		t.Errorf("the warning must state the irreversibility:\n%s", stderr)
	}
}

// TestCertRemoveOfCustomCertDoesNotWarnAboutRevocation avoids crying wolf: only
// letsencrypt certificates are revoked on delete.
func TestCertRemoveOfCustomCertDoesNotWarnAboutRevocation(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	seedCert(h, "other", false)

	stdout, stderr, code := h.run("cert", "rm", "7", "--yes")
	if code != exitcode.OK {
		t.Fatalf("want exit 0, got %d\n%s", code, stderr)
	}
	if strings.Contains(strings.ToLower(stderr), "revoke") {
		t.Errorf("a custom certificate is not revoked; the warning should not appear:\n%s", stderr)
	}
	if _, present := decodeJSON(t, stdout)["revoked"]; present {
		t.Error(`a custom certificate result must not report "revoked"`)
	}
}

// TestCertRemoveRefusesWithDependents: those hosts lose their TLS material on the next
// nginx reload, so the delete names them and demands acknowledgement.
func TestCertRemoveRefusesWithDependents(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	seedCert(h, "letsencrypt", true)

	_, stderr, code := h.run("cert", "rm", "7", "--yes")
	if code != exitcode.Refused {
		t.Fatalf("want exit %d when a host references the cert, got %d\n%s", exitcode.Refused, code, stderr)
	}
	if !strings.Contains(stderr, "proxy-host 12") {
		t.Errorf("the refusal must name the dependent host:\n%s", stderr)
	}
	if muts := h.mutations(); len(muts) != 0 {
		t.Fatalf("revocation proceeded despite dependents: %+v", muts)
	}

	_, stderr, code = h.run("cert", "rm", "7", "--yes", "--cascade-ack")
	if code != exitcode.OK {
		t.Fatalf("want exit 0 with --cascade-ack, got %d\n%s", code, stderr)
	}
}

// TestCertRemoveDryRunListsDependents proves the preview is informative before the
// irreversible step.
func TestCertRemoveDryRunListsDependents(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	seedCert(h, "letsencrypt", true)

	stdout, _, code := h.run("cert", "rm", "7", "--yes", "--dry-run")
	if code != exitcode.OK {
		t.Fatalf("dry run should exit 0, got %d", code)
	}
	out := decodeJSON(t, stdout)
	if out["dry_run"] != true {
		t.Error("dry_run flag missing")
	}
	deps, ok := out["dependents"].([]any)
	if !ok || len(deps) == 0 {
		t.Errorf("the preview must list dependents: %v", out)
	}
	if out["warning"] == nil {
		t.Error("the preview must carry the revocation warning")
	}
	if muts := h.mutations(); len(muts) != 0 {
		t.Fatalf("dry run sent a mutating request: %+v", muts)
	}
}

// TestCertDownloadWritesOutsideCwdAt0600 covers the private-key handling rules: the
// archive contains privkey.pem.
func TestCertDownloadWritesOutsideCwdAt0600(t *testing.T) {
	h := newHarness(t)
	seedCert(h, "letsencrypt", false)
	h.routeFunc("GET", "/api/nginx/certificates/7/download", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write([]byte("PK\x03\x04fake-zip"))
	})

	stdout, stderr, code := h.run("cert", "download", "7")
	if code != exitcode.OK {
		t.Fatalf("want exit 0, got %d\n%s", code, stderr)
	}
	path, _ := decodeJSON(t, stdout)["path"].(string)
	if path == "" {
		t.Fatal("no destination path reported")
	}
	// Default destination is the data directory, never the working directory.
	if cwd, err := os.Getwd(); err == nil && strings.HasPrefix(path, cwd) {
		t.Errorf("archive was written inside the working directory: %s", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("archive mode = %o, want 600 — it contains a private key", perm)
	}
}

// TestCertDownloadRefusesOverwrite: silently replacing key material is never right.
func TestCertDownloadRefusesOverwrite(t *testing.T) {
	h := newHarness(t)
	seedCert(h, "letsencrypt", false)
	h.routeFunc("GET", "/api/nginx/certificates/7/download", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("PK\x03\x04fake-zip"))
	})

	dest := filepath.Join(t.TempDir(), "existing.zip")
	if err := os.WriteFile(dest, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := h.run("cert", "download", "7", "--out", dest)
	if code != exitcode.Refused {
		t.Fatalf("want exit %d on overwrite, got %d\n%s", exitcode.Refused, code, stderr)
	}
	if b, _ := os.ReadFile(dest); string(b) != "old" {
		t.Error("the existing file was modified")
	}
}

// TestCertDownloadRefusesStdout keeps a binary private key out of pipes and, in an
// agent context, out of transcripts.
func TestCertDownloadRefusesStdout(t *testing.T) {
	h := newHarness(t)
	seedCert(h, "letsencrypt", false)

	_, stderr, code := h.run("cert", "download", "7", "--out", "-")
	if code != exitcode.Refused {
		t.Fatalf("want exit %d for stdout, got %d\n%s", exitcode.Refused, code, stderr)
	}
	// Even with the override, a non-terminal stdout is refused.
	_, stderr, code = h.run("cert", "download", "7", "--out", "-", "--force-stdout")
	if code != exitcode.Refused {
		t.Fatalf("non-terminal stdout must still be refused, got %d\n%s", code, stderr)
	}
}

// TestCertDownloadRefusesGitDirectory stops key material from being committed.
func TestCertDownloadRefusesGitDirectory(t *testing.T) {
	h := newHarness(t)
	seedCert(h, "letsencrypt", false)

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := h.run("cert", "download", "7", "--out", filepath.Join(dir, "cert.zip"))
	if code != exitcode.Refused {
		t.Fatalf("want exit %d inside a git repo, got %d\n%s", exitcode.Refused, code, stderr)
	}
	if !strings.Contains(stderr, "git") {
		t.Errorf("the refusal should mention the git repository: %s", stderr)
	}
}

// TestCertUploadDryRunPrintsMetadataOnly: a raw multipart body would bypass the
// key-based scrubber, so previews describe files instead of rendering them.
func TestCertUploadDryRunPrintsMetadataOnly(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	seedCert(h, "other", false)

	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	const secret = "SUPERSECRETKEYMATERIAL"
	if err := os.WriteFile(certPath, []byte("-----BEGIN CERTIFICATE-----\nabc\n-----END CERTIFICATE-----"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("-----BEGIN PRIVATE KEY-----\n"+secret+"\n-----END PRIVATE KEY-----"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := h.run("cert", "upload", "7",
		"--certificate", certPath, "--certificate-key", keyPath, "--yes", "--dry-run")
	if code != exitcode.OK {
		t.Fatalf("dry run should exit 0, got %d\n%s", code, stderr)
	}
	if strings.Contains(stdout, secret) || strings.Contains(stderr, secret) {
		t.Errorf("the dry run printed PEM contents:\n%s\n%s", stdout, stderr)
	}
	if !strings.Contains(stdout, "key.pem") {
		t.Errorf("the dry run should name the files: %s", stdout)
	}
	if !strings.Contains(stdout, "size_bytes") {
		t.Errorf("the dry run should report sizes: %s", stdout)
	}
	if muts := h.mutations(); len(muts) != 0 {
		t.Fatalf("dry run uploaded anyway: %+v", muts)
	}
}

// TestCertCreateRefusedWithoutGate keeps issuance behind the same two factors.
func TestCertCreateRefusedWithoutGate(t *testing.T) {
	h := newHarness(t)
	seedCert(h, "letsencrypt", false)

	_, stderr, code := h.run("cert", "create", "--domain", "new.example.com", "--yes")
	if code != exitcode.Refused {
		t.Fatalf("want exit %d, got %d\n%s", exitcode.Refused, code, stderr)
	}
	if muts := h.mutations(); len(muts) != 0 {
		t.Fatalf("an ACME order was sent while refused: %+v", muts)
	}
}

// TestCertCreateDryRunSendsNothing is the safest possible preview for an operation
// that spends a weekly quota.
func TestCertCreateDryRunSendsNothing(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	seedCert(h, "letsencrypt", false)
	h.route("POST", "/api/nginx/certificates", http.StatusCreated, sampleCert("letsencrypt"))

	stdout, _, code := h.run("cert", "create", "--domain", "new.example.com", "--yes", "--dry-run")
	if code != exitcode.OK {
		t.Fatalf("dry run should exit 0, got %d", code)
	}
	out := decodeJSON(t, stdout)
	if out["dry_run"] != true {
		t.Error("dry_run flag missing")
	}
	body, _ := out["body"].(map[string]any)
	meta, _ := body["meta"].(map[string]any)
	for _, banned := range []string{"letsencrypt_email", "letsencrypt_agree"} {
		if _, present := meta[banned]; present {
			t.Errorf("meta must not contain %q", banned)
		}
	}
	if muts := h.mutations(); len(muts) != 0 {
		t.Fatalf("dry run ordered a certificate: %+v", muts)
	}
}

// TestCertDNSChallengeRequiresProvider catches a misconfiguration locally rather than
// spending an order to discover it.
func TestCertDNSChallengeRequiresProvider(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	seedCert(h, "letsencrypt", false)

	_, stderr, code := h.run("cert", "create", "--domain", "x.example.com", "--dns-challenge", "--yes")
	if code != exitcode.Usage {
		t.Fatalf("want exit %d, got %d\n%s", exitcode.Usage, code, stderr)
	}
}
