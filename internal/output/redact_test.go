package output

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

// The four secret classes the plan requires to never reach an output stream.
const (
	dnsCredential = "sk_live_DNSPROVIDER_SECRET_9f3a"
	userPassword  = "correct-horse-battery-staple"
	bearerToken   = "eyJhbGciOiJSUzUxMiJ9.PAYLOAD.SIGNATURE"
	pemPrivateKey = "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBg\n-----END PRIVATE KEY-----"
)

// secretPayload embeds every secret class in the shapes NPM actually returns
// them: nested under meta, inside a list, and as an HTTP header.
func secretPayload() map[string]any {
	return map[string]any{
		"id":   7,
		"name": "example.com",
		"meta": map[string]any{
			"dns_provider":             "cloudflare",
			"dns_provider_credentials": dnsCredential,
			"certificate_key":          pemPrivateKey,
		},
		"items": []any{
			map[string]any{"username": "admin", "password": userPassword},
		},
		"token":   bearerToken,
		"secret":  userPassword,
		"headers": map[string][]string{"Authorization": {"Bearer " + bearerToken}},
		// A PEM under a key the denylist does not know about, to prove value-level
		// detection also works.
		"uploaded_file": pemPrivateKey,
	}
}

func allSecrets() map[string]string {
	return map[string]string{
		"DNS provider credential": dnsCredential,
		"user password":           userPassword,
		"bearer token":            bearerToken,
		"PEM private key":         "MIIEvQIBADANBg",
	}
}

// TestNoSecretReachesAnyRenderer is the single assertion behind R3: redaction
// lives in the serializer, so it must hold for every format without any renderer
// opting in.
func TestNoSecretReachesAnyRenderer(t *testing.T) {
	for _, f := range []Format{FormatJSON, FormatYAML, FormatTable} {
		t.Run(string(f), func(t *testing.T) {
			var buf bytes.Buffer
			if err := Render(&buf, f, secretPayload()); err != nil {
				t.Fatal(err)
			}
			assertNoSecrets(t, buf.String())
			if !strings.Contains(buf.String(), Placeholder) {
				t.Errorf("output contains no %s marker, so nothing was redacted:\n%s", Placeholder, buf.String())
			}
		})
	}
}

// TestRenderWithColumnsRedacts covers the table path that takes explicit columns,
// which is the one commands actually use for list output.
func TestRenderWithColumnsRedacts(t *testing.T) {
	cols := []Column{
		{Header: "ID", Key: "id"},
		{Header: "CREDS", Key: "meta.dns_provider_credentials"},
		{Header: "KEY", Key: "meta.certificate_key"},
	}
	var buf bytes.Buffer
	if err := RenderWith(&buf, FormatTable, cols, []any{secretPayload()}); err != nil {
		t.Fatal(err)
	}
	assertNoSecrets(t, buf.String())
}

// TestTypedStructIsRedacted guards the normalize step. Scrub cannot see the field
// names of a struct it does not know, so a struct that skipped normalization
// would walk straight past the denylist.
func TestTypedStructIsRedacted(t *testing.T) {
	type cert struct {
		Name  string `json:"name"`
		Token string `json:"token"`
		Meta  struct {
			DNSProviderCredentials string `json:"dns_provider_credentials"`
		} `json:"meta"`
	}
	var c cert
	c.Name = "example.com"
	c.Token = bearerToken
	c.Meta.DNSProviderCredentials = dnsCredential

	for _, f := range []Format{FormatJSON, FormatYAML, FormatTable} {
		var buf bytes.Buffer
		if err := Render(&buf, f, c); err != nil {
			t.Fatal(err)
		}
		assertNoSecrets(t, buf.String())
	}
}

// TestHTTPHeaderRedaction covers the -v transport log, where the bearer token
// would otherwise be printed on every request.
func TestHTTPHeaderRedaction(t *testing.T) {
	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer "+bearerToken)
	hdr.Set("Accept", "application/json")

	scrubbed, ok := Scrub(map[string][]string(hdr)).(map[string][]string)
	if !ok {
		t.Fatalf("Scrub changed the header type")
	}
	if got := scrubbed["Authorization"][0]; got != Placeholder {
		t.Errorf("Authorization = %q, want %q", got, Placeholder)
	}
	if got := scrubbed["Accept"][0]; got != "application/json" {
		t.Errorf("Scrub altered a non-secret header: %q", got)
	}
}

// TestScrubDoesNotMutateInput matters because the undo journal serialises the
// same live objects the renderers receive: if Scrub mutated them, capturing a
// pre-image after rendering would persist "[redacted]" and destroy the restore.
func TestScrubDoesNotMutateInput(t *testing.T) {
	in := secretPayload()
	_ = Scrub(in)

	meta := in["meta"].(map[string]any)
	if meta["dns_provider_credentials"] != dnsCredential {
		t.Fatalf("Scrub mutated its input: credential is now %v", meta["dns_provider_credentials"])
	}
	if in["token"] != bearerToken {
		t.Fatalf("Scrub mutated its input: token is now %v", in["token"])
	}
}

// TestNonSecretValuesSurvive guards against over-redaction: a denylist that eats
// ordinary fields makes the tool useless.
func TestNonSecretValuesSurvive(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatJSON, map[string]any{
		"certificate_id": 5,
		"domain_names":   []any{"app.example.com"},
		"dns_provider":   "cloudflare",
		"nice_name":      "example.com",
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"certificate_id", "5", "app.example.com", "cloudflare", "example.com"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("expected %q to survive redaction:\n%s", want, buf.String())
		}
	}
	if strings.Contains(buf.String(), Placeholder) {
		t.Errorf("nothing here is secret, but something was redacted:\n%s", buf.String())
	}
}

func assertNoSecrets(t *testing.T, out string) {
	t.Helper()
	for name, secret := range allSecrets() {
		if strings.Contains(out, secret) {
			t.Errorf("%s leaked into output:\n%s", name, out)
		}
	}
}
