package npmapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// This file holds the ONLY tests that touch a real NPM instance. They are skipped
// unless NPMCTL_E2E_URL is set, so `go test ./...` stays hermetic — no network, no
// Docker — exactly as the plan's testing strategy requires.
//
// They exist because fixtures only prove the code agrees with itself. A fixture built
// from a wrong payload matches the wrong payload, and two request bodies in this
// project's own plan were wrong before being checked against the schema. The three
// contracts below are BEHAVIOURAL: they are invisible to the schema, so
// `npmctl schema check` cannot detect them drifting.
//
// Run against a disposable instance only:
//
//	docker run -d --name npm-lab -p 18181:81 -p 18080:80 \
//	  -e INITIAL_ADMIN_EMAIL=lab-admin@npmctl.test \
//	  -e INITIAL_ADMIN_PASSWORD=<throwaway> \
//	  jc21/nginx-proxy-manager:2.15.1
//
//	NPMCTL_E2E_URL=http://127.0.0.1:18181 \
//	NPMCTL_E2E_IDENTITY=lab-admin@npmctl.test \
//	NPMCTL_E2E_SECRET=<throwaway> go test ./internal/npmapi/ -run E2E -v
const (
	envE2EURL      = "NPMCTL_E2E_URL"
	envE2EIdentity = "NPMCTL_E2E_IDENTITY"
	envE2ESecret   = "NPMCTL_E2E_SECRET"
	envE2EToken    = "NPMCTL_E2E_TOKEN"
)

// e2eClient builds an authenticated client, or skips.
func e2eClient(t *testing.T) *Client {
	t.Helper()
	url := strings.TrimSpace(os.Getenv(envE2EURL))
	if url == "" {
		t.Skipf("%s is not set; skipping the live smoke test", envE2EURL)
	}
	// Guard against pointing this at production by accident: these tests create and
	// delete real objects.
	if !strings.Contains(url, "127.0.0.1") && !strings.Contains(url, "localhost") {
		if os.Getenv("NPMCTL_E2E_ALLOW_REMOTE") != "1" {
			t.Fatalf("%s is not a loopback address. These tests create and delete real objects; "+
				"set NPMCTL_E2E_ALLOW_REMOTE=1 only for a disposable instance.", envE2EURL)
		}
	}

	anon, err := New(Options{BaseURL: url, Insecure: true, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Confirm we are talking to the pinned version the whole plan is scoped against.
	health, err := anon.Health(ctx)
	if err != nil {
		t.Fatalf("health probe failed: %v", err)
	}
	if health.Version.Major != 2 || health.Version.Minor != 15 {
		t.Logf("WARNING: instance is %d.%d.%d, not the pinned 2.15.1",
			health.Version.Major, health.Version.Minor, health.Version.Revision)
	}

	if tok := strings.TrimSpace(os.Getenv(envE2EToken)); tok != "" {
		c, err := New(Options{BaseURL: url, Token: tok, Insecure: true, Timeout: 30 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	identity := strings.TrimSpace(os.Getenv(envE2EIdentity))
	secret := os.Getenv(envE2ESecret)
	if identity == "" || secret == "" {
		t.Skipf("set %s and %s (or %s) to run the live smoke test", envE2EIdentity, envE2ESecret, envE2EToken)
	}
	tok, challenge, err := anon.RequestToken(ctx, identity, secret)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if challenge != nil {
		t.Skip("the lab account has 2FA enabled; supply NPMCTL_E2E_TOKEN instead")
	}
	c, err := New(Options{BaseURL: url, Token: tok.Token, Insecure: true, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestE2EPartialUpdateLeavesOtherFieldsAlone verifies the partial-update contract.
// A schema check cannot prove this: `minProperties: 1` says a one-field body is
// legal, not that the other fields survive it.
func TestE2EPartialUpdateLeavesOtherFieldsAlone(t *testing.T) {
	c := e2eClient(t)
	ctx := context.Background()

	created, err := c.CreateProxyHost(ctx, map[string]any{
		"domain_names":    []string{"e2e-partial.npmctl.test"},
		"forward_scheme":  "http",
		"forward_host":    "10.0.0.9",
		"forward_port":    8080,
		"block_exploits":  true,
		"caching_enabled": true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = c.DeleteProxyHost(ctx, created.ID) })

	// Change exactly one field.
	updated, err := c.UpdateProxyHost(ctx, created.ID, map[string]any{"forward_port": 9999})
	if err != nil {
		t.Fatalf("partial update: %v", err)
	}
	if updated.ForwardPort != 9999 {
		t.Errorf("forward_port = %d, want 9999", updated.ForwardPort)
	}
	// The fields we did not send must be untouched. If NPM treated a partial body as
	// a full replacement, these would have reverted to defaults.
	if !updated.BlockExploits {
		t.Error("block_exploits was cleared by a partial update that never mentioned it")
	}
	if !updated.CachingEnabled {
		t.Error("caching_enabled was cleared by a partial update that never mentioned it")
	}
	if updated.ForwardHost != "10.0.0.9" {
		t.Errorf("forward_host = %q, want it unchanged", updated.ForwardHost)
	}
}

// TestE2ETLSFlagsAreSilentlyCoerced pins a behavioural contract the schema cannot
// express, and which a fixture would happily contradict.
//
// internal/host.js cleanSslHstsData forces ssl_forced and http2_support off when the host
// has no certificate, then hsts_enabled off without ssl_forced, then hsts_subdomains off
// without hsts_enabled. The write still returns 200. npmctl compares the request against
// the response and warns, because an operator who passes --hsts and sees exit 0 would
// otherwise believe HSTS is on.
func TestE2ETLSFlagsAreSilentlyCoerced(t *testing.T) {
	c := e2eClient(t)
	ctx := context.Background()

	created, err := c.CreateProxyHost(ctx, map[string]any{
		"domain_names":   []string{"e2e-tls.npmctl.test"},
		"forward_scheme": "http",
		"forward_host":   "10.0.0.9",
		"forward_port":   8080,
		// No certificate_id, so every one of these should come back off.
		"ssl_forced":      true,
		"http2_support":   true,
		"hsts_enabled":    true,
		"hsts_subdomains": true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = c.DeleteProxyHost(ctx, created.ID) })

	if created.SSLForced {
		t.Error("ssl_forced was honoured without a certificate; the coercion rule may have changed")
	}
	if created.HTTP2Support {
		t.Error("http2_support was honoured without a certificate; the coercion rule may have changed")
	}
	if created.HSTSEnabled {
		t.Error("hsts_enabled was honoured without ssl_forced; the coercion rule may have changed")
	}
	if created.HSTSSubdomains {
		t.Error("hsts_subdomains was honoured without hsts_enabled; the coercion rule may have changed")
	}
}

// TestE2EUnknownPropertyIsRejected confirms additionalProperties:false is enforced by
// the server, which is what makes the payload builder's allowlist load-bearing.
func TestE2EUnknownPropertyIsRejected(t *testing.T) {
	c := e2eClient(t)
	ctx := context.Background()

	_, err := c.CreateProxyHost(ctx, map[string]any{
		"domain_names":      []string{"e2e-badkey.npmctl.test"},
		"forward_scheme":    "http",
		"forward_host":      "10.0.0.9",
		"forward_port":      8080,
		"letsencrypt_agree": true, // not a permitted property
	})
	if err == nil {
		t.Fatal("the server accepted an unknown property; the payload allowlist may be unnecessary")
	}
	var ae *APIError
	if asErr(err, &ae) && ae.Status != 400 {
		t.Errorf("expected HTTP 400 for an unknown property, got %d: %v", ae.Status, err)
	}
}

// TestE2EEmptyUpdateIsRejected confirms minProperties:1, which is why the payload
// builder refuses an empty body locally with a message that explains itself.
func TestE2EEmptyUpdateIsRejected(t *testing.T) {
	c := e2eClient(t)
	ctx := context.Background()

	created, err := c.CreateProxyHost(ctx, map[string]any{
		"domain_names":   []string{"e2e-empty.npmctl.test"},
		"forward_scheme": "http",
		"forward_host":   "10.0.0.9",
		"forward_port":   8080,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = c.DeleteProxyHost(ctx, created.ID) })

	if _, err := c.UpdateProxyHost(ctx, created.ID, map[string]any{}); err == nil {
		t.Error("the server accepted an empty update body")
	}
}

// TestE2ENginxMetaIsReported confirms the field the write gate's step 7 depends on
// actually exists on a live instance.
func TestE2ENginxMetaIsReported(t *testing.T) {
	c := e2eClient(t)
	ctx := context.Background()

	created, err := c.CreateProxyHost(ctx, map[string]any{
		"domain_names":   []string{"e2e-meta.npmctl.test"},
		"forward_scheme": "http",
		"forward_host":   "10.0.0.9",
		"forward_port":   8080,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = c.DeleteProxyHost(ctx, created.ID) })

	fresh, err := c.GetProxyHost(ctx, created.ID)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	online, present := fresh.Meta.NginxOnline()
	if !present {
		t.Fatal("meta.nginx_online is absent; the post-write health check has nothing to assert on")
	}
	if !online {
		t.Errorf("a freshly created host reports nginx offline: %s", fresh.Meta.NginxErr())
	}
}

// TestE2EAccessListNeverReturnsPasswords is the contract behind refusing
// read-modify-write on access lists. If NPM ever DID return passwords, the refusal
// could be relaxed — so this test states the assumption explicitly.
func TestE2EAccessListNeverReturnsPasswords(t *testing.T) {
	c := e2eClient(t)
	ctx := context.Background()

	const password = "e2e-known-password-1234"
	created, err := c.CreateAccessList(ctx, map[string]any{
		"name":        "e2e-acl",
		"satisfy_any": false,
		"pass_auth":   false,
		"items":       []map[string]any{{"username": "e2euser", "password": password}},
		"clients":     []map[string]any{{"directive": "allow", "address": "10.0.0.0/8"}},
	})
	if err != nil {
		t.Fatalf("create access list: %v", err)
	}
	t.Cleanup(func() { _ = c.DeleteAccessList(ctx, created.ID) })

	fetched, err := c.GetAccessList(ctx, created.ID, "items", "clients")
	if err != nil {
		t.Fatalf("re-read access list: %v", err)
	}
	if len(fetched.Items) == 0 {
		t.Fatal("no items returned")
	}
	for _, item := range fetched.Items {
		if item.Password == password {
			t.Errorf("NPM returned the real password for %q — the read-modify-write refusal "+
				"could be relaxed, and this assumption needs revisiting", item.Username)
		}
		if item.Password != "" {
			t.Errorf("expected an empty password for %q, got %q", item.Username, item.Password)
		}
	}
}

// TestE2EStreamUpdateRejectsDomainNames confirms the POST/PUT asymmetry on a live
// instance rather than trusting the schema read alone.
func TestE2EStreamUpdateRejectsDomainNames(t *testing.T) {
	c := e2eClient(t)
	ctx := context.Background()

	created, err := c.CreateStream(ctx, map[string]any{
		"incoming_port":   32222,
		"forwarding_host": "10.0.0.4",
		"forwarding_port": 22,
		"tcp_forwarding":  true,
	})
	if err != nil {
		t.Fatalf("create stream: %v", err)
	}
	t.Cleanup(func() { _ = c.DeleteStream(ctx, created.ID) })

	if _, err := c.UpdateStream(ctx, created.ID, map[string]any{
		"domain_names": []string{"e2e-stream.npmctl.test"},
	}); err == nil {
		t.Error("PUT /nginx/streams accepted domain_names; StreamUpdateFields may be too strict")
	}
}
