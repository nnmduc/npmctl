package npmapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestCertificateMetaMatchesSchema is the regression guard for the exact bug the
// review found: an earlier draft sent letsencrypt_email and letsencrypt_agree, and
// certificate meta is additionalProperties:false, so the request would have failed
// on first contact with a live instance.
func TestCertificateMetaMatchesSchema(t *testing.T) {
	props := schemaCertificateMetaProperties(t)
	have := map[string]bool{}
	for _, f := range CertificateMetaFields {
		have[f] = true
	}
	for name := range props {
		if !have[name] {
			t.Errorf("schema permits meta.%s but CertificateMetaFields omits it", name)
		}
	}
	for _, f := range CertificateMetaFields {
		if _, ok := props[f]; !ok {
			t.Errorf("CertificateMetaFields lists meta.%s but the schema forbids it", f)
		}
	}
	// Name the two specific keys that caused the bug, so the guard is explicit.
	for _, banned := range []string{"letsencrypt_email", "letsencrypt_agree"} {
		if have[banned] {
			t.Errorf("meta.%s is not a permitted key and must never be sent", banned)
		}
	}
}

// schemaCertificateMetaProperties reads the permitted meta keys from the vendored
// schema.
func schemaCertificateMetaProperties(t *testing.T) map[string]any {
	t.Helper()
	props := schemaRequestProperties(t, "/nginx/certificates", "post")
	meta, ok := props["meta"].(map[string]any)
	if !ok {
		t.Fatal("certificate POST schema has no meta property")
	}
	if meta["additionalProperties"] != false {
		t.Error("certificate meta should forbid additional properties")
	}
	inner, _ := meta["properties"].(map[string]any)
	return inner
}

// TestCertificateMetaBuilderRejectsBannedKeys proves the builder enforces it rather
// than relying on the caller to remember.
func TestCertificateMetaBuilderRejectsBannedKeys(t *testing.T) {
	for _, banned := range []string{"letsencrypt_email", "letsencrypt_agree"} {
		t.Run(banned, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("setting meta.%s must be refused", banned)
				}
			}()
			NewCertificateMetaPayload().Set(banned, "x")
		})
	}
}

// TestCertificateCreateBodyShape locks the request the CLI actually sends.
func TestCertificateCreateBodyShape(t *testing.T) {
	p := NewCertificatePayload()
	p.Set("provider", "letsencrypt")
	p.Set("domain_names", []string{"example.com"})
	meta := NewCertificateMetaPayload()
	meta.Set("dns_challenge", false)
	m, err := meta.Map()
	if err != nil {
		t.Fatal(err)
	}
	p.Set("meta", m)

	body, err := p.Map()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(body)
	for _, banned := range []string{"letsencrypt_email", "letsencrypt_agree"} {
		if strings.Contains(string(raw), banned) {
			t.Errorf("serialized body contains %s: %s", banned, raw)
		}
	}
	if body["provider"] != "letsencrypt" {
		t.Errorf("provider missing: %v", body)
	}
}

// TestAccessListFieldsMatchSchema guards the second payload bug the review found:
// access_items/access_clients are shared definition names, not body properties.
func TestAccessListFieldsMatchSchema(t *testing.T) {
	props := schemaRequestProperties(t, "/nginx/access-lists/{listID}", "put")
	have := map[string]bool{}
	for _, f := range AccessListFields {
		have[f] = true
	}
	for name := range props {
		if !have[name] {
			t.Errorf("schema permits %q on an access-list write but AccessListFields omits it", name)
		}
	}
	for _, banned := range []string{"access_items", "access_clients"} {
		if have[banned] {
			t.Errorf("%q is a shared definition name, not a request property — it must never be sent", banned)
		}
		if _, ok := props[banned]; ok {
			t.Errorf("unexpected: schema now accepts %q; re-check the payload builder", banned)
		}
	}
	for _, want := range []string{"items", "clients"} {
		if !have[want] {
			t.Errorf("AccessListFields must include %q", want)
		}
	}
}

// TestStreamUpdateRejectsDomainNames documents a real asymmetry: POST /nginx/streams
// accepts domain_names, PUT does not, and both forbid unknown properties.
func TestStreamUpdateRejectsDomainNames(t *testing.T) {
	createProps := schemaRequestProperties(t, "/nginx/streams", "post")
	updateProps := schemaRequestProperties(t, "/nginx/streams/{streamID}", "put")

	if _, ok := createProps["domain_names"]; !ok {
		t.Error("stream POST should accept domain_names")
	}
	if _, ok := updateProps["domain_names"]; ok {
		t.Error("stream PUT is not expected to accept domain_names; re-check StreamUpdateFields")
	}
	for _, f := range StreamUpdateFields {
		if f == "domain_names" {
			t.Error("StreamUpdateFields must not include domain_names")
		}
	}
	defer func() {
		if recover() == nil {
			t.Fatal("setting domain_names on a stream update must be refused")
		}
	}()
	NewStreamUpdatePayload().Set("domain_names", []string{"x.example.com"})
}

// TestRedirectAndDeadHostFieldsMatchSchema keeps the remaining two field lists honest.
func TestRedirectAndDeadHostFieldsMatchSchema(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		fields []string
	}{
		{"redirection-host", "/nginx/redirection-hosts/{hostID}", RedirectionHostFields},
		{"dead-host", "/nginx/dead-hosts/{hostID}", DeadHostFields},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := schemaRequestProperties(t, tc.path, "put")
			have := map[string]bool{}
			for _, f := range tc.fields {
				have[f] = true
			}
			for name := range props {
				if !have[name] {
					t.Errorf("schema permits %q but the field list omits it", name)
				}
			}
			for _, f := range tc.fields {
				if _, ok := props[f]; !ok {
					t.Errorf("field list has %q but the schema forbids it", f)
				}
			}
		})
	}
}

// TestDNSChallengeTimeout: a flat HTTP-01 budget aborts a DNS-01 order that is
// working normally, because DNS-01 must wait for propagation before validation.
func TestDNSChallengeTimeout(t *testing.T) {
	if got := DNSChallengeTimeout(0); got != 240*time.Second {
		t.Errorf("DNSChallengeTimeout(0) = %v, want 240s", got)
	}
	if got := DNSChallengeTimeout(120); got != 360*time.Second {
		t.Errorf("DNSChallengeTimeout(120) = %v, want propagation + 240s", got)
	}
	if CertificateTimeout != 180*time.Second {
		t.Errorf("CertificateTimeout = %v, want 180s for HTTP-01", CertificateTimeout)
	}
}

// pollServer serves a certificate list that changes over successive reads.
func pollServer(t *testing.T, states [][]Certificate) *httptest.Server {
	t.Helper()
	// Keep the suite fast: the interval only governs how long we idle between reads.
	prev := pollInterval
	pollInterval = 10 * time.Millisecond
	t.Cleanup(func() { pollInterval = prev })
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		idx := call
		if idx >= len(states) {
			idx = len(states) - 1
		}
		call++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(states[idx])
	}))
	t.Cleanup(srv.Close)
	return srv
}

func certFor(domains []string, expires string) Certificate {
	return Certificate{ID: 5, Provider: "letsencrypt", DomainNames: domains, ExpiresOn: expires}
}

// TestPollReportsIssued is the unambiguous success case: row present WITH an expiry.
func TestPollReportsIssued(t *testing.T) {
	want := []string{"example.com"}
	srv := pollServer(t, [][]Certificate{{certFor(want, "2027-01-01 00:00:00")}})
	c, _ := New(Options{BaseURL: srv.URL, Token: "t"})

	res, err := c.PollIssuance(context.Background(), want, time.Now().Add(time.Minute), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != StateIssued {
		t.Fatalf("state = %s, want %s (detail: %s)", res.State, StateIssued, res.Detail)
	}
}

// TestPollReportsFailureWhenRowDisappears is why polling distinguishes "row absent"
// from "row present without expires_on": NPM creates the row, runs certbot, and
// DELETES the row on failure.
func TestPollReportsFailureWhenRowDisappears(t *testing.T) {
	want := []string{"example.com"}
	srv := pollServer(t, [][]Certificate{
		{certFor(want, "")}, // row created, certbot running
		{},                  // certbot failed, row removed
	})
	c, _ := New(Options{BaseURL: srv.URL, Token: "t"})

	res, err := c.PollIssuance(context.Background(), want, time.Now().Add(30*time.Second), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != StateNotPresent {
		t.Fatalf("state = %s, want %s (detail: %s)", res.State, StateNotPresent, res.Detail)
	}
}

// TestPollReportsIndeterminateAtDeadline must never say "may have succeeded": an
// ambiguous answer is what drives a retry, and a retry spends ACME quota.
func TestPollReportsIndeterminateAtDeadline(t *testing.T) {
	want := []string{"example.com"}
	srv := pollServer(t, [][]Certificate{{certFor(want, "")}})
	c, _ := New(Options{BaseURL: srv.URL, Token: "t"})

	// Deadline already passed: one read, then a verdict.
	res, err := c.PollIssuance(context.Background(), want, time.Now().Add(-time.Second), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != StateIndeterminate {
		t.Fatalf("state = %s, want %s", res.State, StateIndeterminate)
	}
	if !strings.Contains(res.Detail, "Do not retry before") {
		t.Errorf("an indeterminate result must name a safe retry time: %s", res.Detail)
	}
	if strings.Contains(strings.ToLower(res.Detail), "may have succeeded") {
		t.Errorf("the detail must not be ambiguous: %s", res.Detail)
	}
}

// TestPollIgnoresUnrelatedCertificates keeps the match exact.
func TestPollIgnoresUnrelatedCertificates(t *testing.T) {
	srv := pollServer(t, [][]Certificate{{certFor([]string{"other.example.com"}, "2027-01-01 00:00:00")}})
	c, _ := New(Options{BaseURL: srv.URL, Token: "t"})

	res, err := c.PollIssuance(context.Background(), []string{"example.com"}, time.Now().Add(-time.Second), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != StateNotPresent {
		t.Fatalf("state = %s, want %s", res.State, StateNotPresent)
	}
}

// TestIsLetsEncryptDrivesRevocationWarning: provider is the only signal that a delete
// will revoke rather than merely unlink.
func TestIsLetsEncryptDrivesRevocationWarning(t *testing.T) {
	le := &Certificate{Provider: "letsencrypt"}
	custom := &Certificate{Provider: "other"}
	if !le.IsLetsEncrypt() {
		t.Error("a letsencrypt certificate must be flagged as revoking on delete")
	}
	if custom.IsLetsEncrypt() {
		t.Error("a custom certificate must not be flagged as revoking")
	}
}

// TestMultipartBodyNeverLogged covers the gap a key-based denylist cannot close.
func TestMultipartBodyNeverLogged(t *testing.T) {
	const key = "-----BEGIN PRIVATE KEY-----\nSECRETKEYMATERIAL\n-----END PRIVATE KEY-----"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	var log strings.Builder
	c, _ := New(Options{BaseURL: srv.URL, Token: "t", Verbose: true, VerboseTo: &log})
	files := &CertificateFiles{
		Certificate:    &PEMFile{Field: "certificate", Filename: "cert.pem", Data: []byte("-----BEGIN CERTIFICATE-----\nx\n-----END CERTIFICATE-----")},
		CertificateKey: &PEMFile{Field: "certificate_key", Filename: "key.pem", Data: []byte(key)},
	}
	if _, err := c.UploadCertificate(context.Background(), 5, files); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(log.String(), "SECRETKEYMATERIAL") {
		t.Errorf("-v logged the multipart body containing a private key:\n%s", log.String())
	}
	if !strings.Contains(log.String(), "body not logged") {
		t.Errorf("expected the log to note the body was withheld:\n%s", log.String())
	}
}

// TestDescribeNeverIncludesContents backs the preview rule for uploads.
func TestDescribeNeverIncludesContents(t *testing.T) {
	const key = "-----BEGIN PRIVATE KEY-----\nSECRETKEYMATERIAL\n-----END PRIVATE KEY-----"
	files := &CertificateFiles{
		Certificate:    &PEMFile{Field: "certificate", Filename: "cert.pem", Data: []byte("-----BEGIN CERTIFICATE-----\nx")},
		CertificateKey: &PEMFile{Field: "certificate_key", Filename: "key.pem", Data: []byte(key)},
	}
	raw, _ := json.Marshal(files.Describe())
	if strings.Contains(string(raw), "SECRETKEYMATERIAL") {
		t.Errorf("Describe leaked file contents: %s", raw)
	}
	if !strings.Contains(string(raw), "key.pem") || !strings.Contains(string(raw), "private key") {
		t.Errorf("Describe should report filename and detected kind: %s", raw)
	}
}

// TestValidateRejectsSwappedFiles catches an easy mix-up before a round trip.
func TestValidateRejectsSwappedFiles(t *testing.T) {
	files := &CertificateFiles{
		Certificate:    &PEMFile{Field: "certificate", Filename: "key.pem", Data: []byte("-----BEGIN PRIVATE KEY-----\nx")},
		CertificateKey: &PEMFile{Field: "certificate_key", Filename: "cert.pem", Data: []byte("-----BEGIN CERTIFICATE-----\nx")},
	}
	if err := files.Validate(); err == nil {
		t.Fatal("swapping the certificate and key must be caught locally")
	}
}
