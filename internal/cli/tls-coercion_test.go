package cli

import (
	"net/http"
	"strings"
	"testing"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

// TestWarnTLSCoercionReportsSilentlyDroppedFlags covers the unit directly.
func TestWarnTLSCoercionReportsSilentlyDroppedFlags(t *testing.T) {
	requested := map[string]any{
		"ssl_forced":      true,
		"http2_support":   true,
		"hsts_enabled":    true,
		"hsts_subdomains": true,
		"forward_port":    9090,
	}
	applied := map[string]any{
		"ssl_forced":      false,
		"http2_support":   false,
		"hsts_enabled":    false,
		"hsts_subdomains": false,
		"forward_port":    9090,
	}
	var sb strings.Builder
	warnTLSCoercion(&sb, requested, applied)
	out := sb.String()

	for _, flag := range []string{"ssl_forced", "http2_support", "hsts_enabled", "hsts_subdomains"} {
		if !strings.Contains(out, flag) {
			t.Errorf("warning omits %s:\n%s", flag, out)
		}
	}
	// Each line must say WHY, or the operator cannot act on it.
	if !strings.Contains(out, "--certificate-id") {
		t.Errorf("the warning should name the missing prerequisite:\n%s", out)
	}
}

// TestWarnTLSCoercionSilentWhenHonoured avoids crying wolf on a normal write.
func TestWarnTLSCoercionSilentWhenHonoured(t *testing.T) {
	var sb strings.Builder
	warnTLSCoercion(&sb,
		map[string]any{"ssl_forced": true, "hsts_enabled": true},
		map[string]any{"ssl_forced": true, "hsts_enabled": true},
	)
	if sb.String() != "" {
		t.Errorf("no coercion occurred, but a warning was printed:\n%s", sb.String())
	}
}

// TestWarnTLSCoercionIgnoresFlagsNotRequested: a field left off by the caller is not a
// coercion, it is simply absent.
func TestWarnTLSCoercionIgnoresFlagsNotRequested(t *testing.T) {
	var sb strings.Builder
	warnTLSCoercion(&sb,
		map[string]any{"forward_port": 8080},
		map[string]any{"forward_port": 8080, "ssl_forced": false, "hsts_enabled": false},
	)
	if sb.String() != "" {
		t.Errorf("unrequested flags must not be reported:\n%s", sb.String())
	}
}

// TestWarnTLSCoercionIgnoresExplicitlyDisabled: asking for false and getting false is
// agreement, not coercion.
func TestWarnTLSCoercionIgnoresExplicitlyDisabled(t *testing.T) {
	var sb strings.Builder
	warnTLSCoercion(&sb,
		map[string]any{"ssl_forced": false},
		map[string]any{"ssl_forced": false},
	)
	if sb.String() != "" {
		t.Errorf("explicitly disabling a flag is not a coercion:\n%s", sb.String())
	}
}

// TestHostCreateWarnsOnCoercedFlags wires the check through a real command, so the
// warning cannot be lost by a refactor.
func TestHostCreateWarnsOnCoercedFlags(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()

	// The server answers 200 with the flags forced off, exactly as NPM does.
	coerced := sampleHost()
	coerced["ssl_forced"] = false
	coerced["hsts_enabled"] = false
	coerced["http2_support"] = false
	coerced["certificate_id"] = 0
	h.route("POST", "/api/nginx/proxy-hosts", http.StatusCreated, coerced)
	h.route("GET", "/api/nginx/proxy-hosts/12", http.StatusOK, coerced)

	_, stderr, code := h.run("host", "create",
		"--domain", "new.example.com", "--forward-host", "10.0.0.5", "--forward-port", "80",
		"--ssl-forced", "--hsts", "--http2", "--yes")
	if code != exitcode.OK {
		t.Fatalf("want exit 0, got %d\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "did not apply") {
		t.Errorf("the command must report silently dropped TLS flags:\n%s", stderr)
	}
}
