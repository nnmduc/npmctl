package npmapi

import (
	"encoding/json"
	"testing"
)

// TestPayloadRejectsUnknownKey guards additionalProperties:false. NPM answers
// "should NOT have additional properties", which does not say which key was wrong;
// failing here names it.
func TestPayloadRejectsUnknownKey(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("setting a key outside the schema must not be silently accepted")
		}
	}()
	NewProxyHostPayload().Set("letsencrypt_agree", true)
}

// TestPayloadRejectsEmptyBody guards minProperties:1 on PUT.
func TestPayloadRejectsEmptyBody(t *testing.T) {
	if _, err := NewProxyHostPayload().Map(); err == nil {
		t.Fatal("an empty update body must be refused before it reaches the API")
	}
}

// TestPayloadEmitsOnlySetFields is what makes a partial update safe: sending a
// field the caller never mentioned would overwrite server state with a Go zero
// value.
func TestPayloadEmitsOnlySetFields(t *testing.T) {
	p := NewProxyHostPayload()
	p.Set("forward_port", 9090)

	body, err := p.Map()
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 {
		t.Fatalf("body has %d fields, want exactly the one that was set: %v", len(body), body)
	}
	if body["forward_port"] != 9090 {
		t.Fatalf("forward_port = %v", body["forward_port"])
	}
}

// TestSetIfSkipsUnsetFlags mirrors how commands bind cobra's Changed().
func TestSetIfSkipsUnsetFlags(t *testing.T) {
	p := NewProxyHostPayload()
	p.SetIf(false, "ssl_forced", false)
	p.SetIf(true, "enabled", false)

	body, err := p.Map()
	if err != nil {
		t.Fatal(err)
	}
	if _, present := body["ssl_forced"]; present {
		t.Error("an unset flag must not appear in the body")
	}
	// Explicitly setting false must survive: "set false" and "unset" are different
	// intents, and conflating them is how a tool silently re-enables settings.
	if v, present := body["enabled"]; !present || v != false {
		t.Errorf("enabled should be present and false, got %v (present=%v)", v, present)
	}
}

// TestProxyHostFieldsMatchSchema pins the allowlist to the vendored schema, so a
// field added or renamed upstream is caught by `schema check` rather than a 400.
func TestProxyHostFieldsMatchSchema(t *testing.T) {
	props := schemaRequestProperties(t, "/nginx/proxy-hosts/{hostID}", "put")
	have := map[string]bool{}
	for _, f := range ProxyHostFields {
		have[f] = true
	}
	for name := range props {
		if !have[name] {
			t.Errorf("schema permits %q on a proxy-host write but ProxyHostFields omits it", name)
		}
	}
	for _, f := range ProxyHostFields {
		if _, ok := props[f]; !ok {
			t.Errorf("ProxyHostFields lists %q but the schema does not permit it", f)
		}
	}
}

// TestCertificateIDAcceptsNew documents the anyOf[integer, "new"] shape: a
// proxy-host write carrying "new" triggers a blocking ACME order.
func TestCertificateIDAcceptsNew(t *testing.T) {
	p := NewProxyHostPayload()
	p.Set("certificate_id", "new")
	body, err := p.Map()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(body)
	if string(b) != `{"certificate_id":"new"}` {
		t.Fatalf(`certificate_id must serialise as the string "new", got %s`, b)
	}
}
