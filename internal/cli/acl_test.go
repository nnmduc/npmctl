package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

// sampleACL has two existing users. NPM returns their passwords as empty strings —
// that is the behaviour every assertion here turns on.
func sampleACL() map[string]any {
	return map[string]any{
		"id":               3,
		"created_on":       "2026-01-01T00:00:00.000Z",
		"modified_on":      "2026-01-02T00:00:00.000Z",
		"name":             "staff",
		"satisfy_any":      false,
		"pass_auth":        false,
		"proxy_host_count": 2,
		"items": []any{
			map[string]any{"id": 1, "username": "alice", "password": "", "hint": "a****"},
			map[string]any{"id": 2, "username": "bob", "password": "", "hint": "b****"},
		},
		"clients": []any{
			map[string]any{"id": 1, "directive": "allow", "address": "10.0.0.0/8"},
		},
	}
}

func seedACL(h *harness) {
	h.route("GET", "/api/nginx/access-lists", http.StatusOK, []any{sampleACL()})
	h.route("GET", "/api/nginx/access-lists/3", http.StatusOK, sampleACL())
}

// TestACLUpdateUsesItemsAndClients is the regression guard for the second payload bug
// the review found: access_items/access_clients are shared definition names in
// common.json, not request-body properties, and sending them returns 400.
func TestACLUpdateUsesItemsAndClients(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	seedACL(h)

	var body string
	h.routeFunc("PUT", "/api/nginx/access-lists/3", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sampleACL())
	})

	_, stderr, code := h.run("acl", "update", "3",
		"--item", "alice:realpassword", "--client", "allow:10.0.0.0/8", "--yes")
	if code != exitcode.OK {
		t.Fatalf("want exit 0, got %d\n%s", code, stderr)
	}
	for _, banned := range []string{"access_items", "access_clients"} {
		if strings.Contains(body, banned) {
			t.Errorf("request body contains %q, which the API rejects: %s", banned, body)
		}
	}
	if !strings.Contains(body, `"items"`) || !strings.Contains(body, `"clients"`) {
		t.Errorf("request body must use items/clients: %s", body)
	}
}

// TestACLUpdateRefusesBlankingExistingPassword is the central protection. NPM accepts
// an empty password and answers 200, so nothing downstream would flag it: three real
// users would silently end up with blank credentials.
func TestACLUpdateRefusesBlankingExistingPassword(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	seedACL(h)
	h.route("PUT", "/api/nginx/access-lists/3", http.StatusOK, sampleACL())

	// `alice:` — an existing user with an empty password.
	_, stderr, code := h.run("acl", "update", "3", "--item", "alice:", "--yes")
	if code != exitcode.Usage && code != exitcode.Refused {
		t.Fatalf("want a refusal, got exit %d\n%s", code, stderr)
	}
	if muts := h.mutations(); len(muts) != 0 {
		t.Fatalf("a password-blanking update was sent: %+v", muts)
	}
	if !strings.Contains(stderr, "password") {
		t.Errorf("the refusal should explain the password problem: %s", stderr)
	}
}

// TestACLUpdateDiffNamesEveryChange: a full-replacement update is only safe if the
// operator can see what it replaces.
func TestACLUpdateDiffNamesEveryChange(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	seedACL(h)
	h.route("PUT", "/api/nginx/access-lists/3", http.StatusOK, sampleACL())

	// Keep alice (with a password), drop bob, add carol.
	_, stderr, code := h.run("acl", "update", "3",
		"--item", "alice:newpass", "--item", "carol:carolpass", "--yes", "--dry-run")
	if code != exitcode.OK {
		t.Fatalf("dry run should exit 0, got %d\n%s", code, stderr)
	}
	for _, want := range []string{"ADDED", "carol", "REMOVED", "bob", "alice"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("item diff is missing %q:\n%s", want, stderr)
		}
	}
}

// TestACLDeleteRefusesWithDependents: hosts referencing the list lose their access
// control on the next nginx reload.
func TestACLDeleteRefusesWithDependents(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	seedACL(h)
	h.route("DELETE", "/api/nginx/access-lists/3", http.StatusOK, nil)

	_, stderr, code := h.run("acl", "rm", "3", "--yes")
	if code != exitcode.Refused {
		t.Fatalf("want exit %d when hosts still use the list, got %d\n%s", exitcode.Refused, code, stderr)
	}
	if !strings.Contains(stderr, "--cascade-ack") {
		t.Errorf("the refusal should name --cascade-ack: %s", stderr)
	}
	if muts := h.mutations(); len(muts) != 0 {
		t.Fatalf("delete proceeded despite dependents: %+v", muts)
	}

	// With acknowledgement it goes through.
	_, stderr, code = h.run("acl", "rm", "3", "--yes", "--cascade-ack")
	if code != exitcode.OK {
		t.Fatalf("want exit 0 with --cascade-ack, got %d\n%s", code, stderr)
	}
}

// TestACLResolvesByName keeps the "never invent IDs" rule usable.
func TestACLResolvesByName(t *testing.T) {
	h := newHarness(t)
	seedACL(h)

	stdout, stderr, code := h.run("acl", "get", "staff")
	if code != exitcode.OK {
		t.Fatalf("want exit 0, got %d\n%s", code, stderr)
	}
	if got := decodeJSON(t, stdout)["id"]; got != float64(3) {
		t.Errorf("resolved to id %v, want 3", got)
	}
}

// TestACLItemParsingRejectsMalformedInput surfaces mistakes as usage errors.
func TestACLItemParsingRejectsMalformedInput(t *testing.T) {
	cases := []struct{ name, flag, value string }{
		{"item without colon", "--item", "aliceonly"},
		{"client without colon", "--client", "10.0.0.0/8"},
		{"client bad directive", "--client", "permit:10.0.0.0/8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.allowWrites()
			seedACL(h)
			_, _, code := h.run("acl", "create", "--name", "x", tc.flag, tc.value, "--yes")
			if code != exitcode.Usage {
				t.Fatalf("want exit %d, got %d", exitcode.Usage, code)
			}
		})
	}
}

// TestACLHasNoAddRemoveItemFlags: those helpers would require read-modify-write, and
// GET never returns passwords, so they cannot be implemented safely in v1.
func TestACLHasNoAddRemoveItemFlags(t *testing.T) {
	root, _ := NewRootCommand()
	for _, path := range [][]string{{"acl", "update"}, {"acl", "create"}} {
		cmd, _, err := root.Find(path)
		if err != nil {
			t.Fatalf("command %v not found: %v", path, err)
		}
		for _, banned := range []string{"add-item", "remove-item"} {
			if cmd.Flags().Lookup(banned) != nil {
				t.Errorf("%v must not offer --%s: it cannot preserve existing passwords", path, banned)
			}
		}
	}
}
