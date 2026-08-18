package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

func sampleRedirect() map[string]any {
	return map[string]any{
		"id": 4, "created_on": "2026-01-01T00:00:00.000Z", "modified_on": "2026-01-02T00:00:00.000Z",
		"domain_names": []string{"old.example.com"}, "forward_scheme": "auto",
		"forward_domain_name": "new.example.com", "forward_http_code": 301,
		"preserve_path": true, "enabled": true, "certificate_id": 0,
		"meta": map[string]any{"nginx_online": true, "nginx_err": nil},
	}
}

func sampleDeadHost() map[string]any {
	return map[string]any{
		"id": 5, "created_on": "2026-01-01T00:00:00.000Z", "modified_on": "2026-01-02T00:00:00.000Z",
		"domain_names": []string{"parked.example.com"}, "enabled": true, "certificate_id": 0,
		"meta": map[string]any{"nginx_online": true, "nginx_err": nil},
	}
}

func sampleStream() map[string]any {
	return map[string]any{
		"id": 6, "created_on": "2026-01-01T00:00:00.000Z", "modified_on": "2026-01-02T00:00:00.000Z",
		"incoming_port": 2222, "forwarding_host": "10.0.0.4", "forwarding_port": 22,
		"tcp_forwarding": true, "udp_forwarding": false, "enabled": true, "certificate_id": 0,
		"meta": map[string]any{"nginx_online": true, "nginx_err": nil},
	}
}

// crudCase describes one collection's happy path and its write surface.
type crudCase struct {
	group    string
	basePath string
	id       string
	sample   func() map[string]any
	create   []string
	update   []string
}

var crudCases = []crudCase{
	{
		group: "redirect", basePath: "/api/nginx/redirection-hosts", id: "4", sample: sampleRedirect,
		create: []string{"--domain", "old.example.com", "--forward-domain", "new.example.com"},
		update: []string{"--http-code", "302"},
	},
	{
		group: "dead-host", basePath: "/api/nginx/dead-hosts", id: "5", sample: sampleDeadHost,
		create: []string{"--domain", "parked.example.com"},
		update: []string{"--http2"},
	},
	{
		group: "stream", basePath: "/api/nginx/streams", id: "6", sample: sampleStream,
		create: []string{"--incoming-port", "2222", "--forwarding-host", "10.0.0.4", "--forwarding-port", "22"},
		update: []string{"--forwarding-port", "2200"},
	},
}

func seedCRUD(h *harness, tc crudCase) {
	h.route("GET", tc.basePath, http.StatusOK, []any{tc.sample()})
	h.route("GET", tc.basePath+"/"+tc.id, http.StatusOK, tc.sample())
	h.route("POST", tc.basePath, http.StatusCreated, tc.sample())
	h.route("PUT", tc.basePath+"/"+tc.id, http.StatusOK, tc.sample())
	h.route("DELETE", tc.basePath+"/"+tc.id, http.StatusOK, nil)
	h.route("POST", tc.basePath+"/"+tc.id+"/enable", http.StatusOK, nil)
	h.route("POST", tc.basePath+"/"+tc.id+"/disable", http.StatusOK, nil)
}

// TestCRUDGroupsSupportEveryOperation walks all seven operations per collection.
func TestCRUDGroupsSupportEveryOperation(t *testing.T) {
	for _, tc := range crudCases {
		t.Run(tc.group, func(t *testing.T) {
			h := newHarness(t)
			h.allowWrites()
			seedCRUD(h, tc)

			ops := [][]string{
				{tc.group, "list"},
				{tc.group, "get", tc.id},
				append([]string{tc.group, "create"}, append(tc.create, "--yes")...),
				append([]string{tc.group, "update", tc.id}, append(tc.update, "--yes")...),
				{tc.group, "enable", tc.id, "--yes"},
				{tc.group, "disable", tc.id, "--yes"},
				{tc.group, "rm", tc.id, "--yes"},
			}
			for _, args := range ops {
				_, stderr, code := h.run(args...)
				if code != exitcode.OK {
					t.Errorf("%v exited %d\n%s", args, code, stderr)
				}
			}
		})
	}
}

// TestCRUDWritesAreGated asserts the two-factor gate across every generated write.
func TestCRUDWritesAreGated(t *testing.T) {
	for _, tc := range crudCases {
		t.Run(tc.group, func(t *testing.T) {
			h := newHarness(t)
			seedCRUD(h, tc) // no allowWrites
			writes := [][]string{
				append([]string{tc.group, "create"}, append(tc.create, "--yes")...),
				append([]string{tc.group, "update", tc.id}, append(tc.update, "--yes")...),
				{tc.group, "rm", tc.id, "--yes"},
				{tc.group, "enable", tc.id, "--yes"},
				{tc.group, "disable", tc.id, "--yes"},
			}
			for _, args := range writes {
				_, stderr, code := h.run(args...)
				if code != exitcode.Refused {
					t.Errorf("%v exited %d, want %d (refused)\n%s", args, code, exitcode.Refused, stderr)
				}
			}
			if muts := h.mutations(); len(muts) != 0 {
				t.Errorf("refused writes still sent mutating requests: %+v", muts)
			}
		})
	}
}

// TestCRUDDryRunSendsNoMutation asserts by method, per collection.
func TestCRUDDryRunSendsNoMutation(t *testing.T) {
	for _, tc := range crudCases {
		t.Run(tc.group, func(t *testing.T) {
			h := newHarness(t)
			h.allowWrites()
			seedCRUD(h, tc)
			writes := [][]string{
				append([]string{tc.group, "create"}, append(tc.create, "--yes", "--dry-run")...),
				append([]string{tc.group, "update", tc.id}, append(tc.update, "--yes", "--dry-run")...),
				{tc.group, "rm", tc.id, "--yes", "--dry-run"},
			}
			for _, args := range writes {
				stdout, stderr, code := h.run(args...)
				if code != exitcode.OK {
					t.Errorf("%v exited %d\n%s", args, code, stderr)
					continue
				}
				if got := decodeJSON(t, stdout)["dry_run"]; got != true {
					t.Errorf("%v: dry_run missing from output", args)
				}
			}
			if muts := h.mutations(); len(muts) != 0 {
				t.Errorf("dry runs sent mutating requests: %+v", muts)
			}
		})
	}
}

// TestCRUDCapturesPreImage checks the journal for every collection's update.
func TestCRUDCapturesPreImage(t *testing.T) {
	for _, tc := range crudCases {
		t.Run(tc.group, func(t *testing.T) {
			h := newHarness(t)
			h.allowWrites()
			seedCRUD(h, tc)

			args := append([]string{tc.group, "update", tc.id}, append(tc.update, "--yes")...)
			if _, stderr, code := h.run(args...); code != exitcode.OK {
				t.Fatalf("update failed with %d\n%s", code, stderr)
			}
			if got := len(journalFiles(t, h)); got != 1 {
				t.Errorf("journal holds %d entries, want 1", got)
			}
		})
	}
}

// TestDeadHost404Alias: `404` is the name most operators reach for.
func TestDeadHost404Alias(t *testing.T) {
	h := newHarness(t)
	seedCRUD(h, crudCases[1])

	stdout, stderr, code := h.run("404", "list")
	if code != exitcode.OK {
		t.Fatalf("the 404 alias should resolve to dead-host, got %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "parked.example.com") {
		t.Errorf("unexpected output: %s", stdout)
	}
}

// TestStreamUpdateRefusesDomainFlag surfaces NPM's own asymmetry as a usage error
// instead of a 400: PUT /nginx/streams rejects domain_names.
func TestStreamUpdateRefusesDomainFlag(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	seedCRUD(h, crudCases[2])

	_, stderr, code := h.run("stream", "update", "6", "--domain", "x.example.com", "--yes")
	if code != exitcode.Usage {
		t.Fatalf("want exit %d, got %d\n%s", exitcode.Usage, code, stderr)
	}
	if !strings.Contains(stderr, "domain") {
		t.Errorf("the error should explain the domain restriction: %s", stderr)
	}
	if muts := h.mutations(); len(muts) != 0 {
		t.Fatalf("a request the API would reject was still sent: %+v", muts)
	}
}

// TestStreamResolvesByIncomingPort: streams need not carry a domain, so a port is the
// natural handle.
func TestStreamResolvesByIncomingPort(t *testing.T) {
	h := newHarness(t)
	h.route("GET", "/api/nginx/streams", http.StatusOK, []any{sampleStream()})
	// The ID lookup misses; resolution falls back to matching the incoming port.
	h.route("GET", "/api/nginx/streams/2222", http.StatusNotFound,
		map[string]any{"error": map[string]any{"code": 404, "message": "not found"}})

	stdout, stderr, code := h.run("stream", "get", "2222")
	if code != exitcode.OK {
		t.Fatalf("want exit 0, got %d\n%s", code, stderr)
	}
	if got := decodeJSON(t, stdout)["id"]; got != float64(6) {
		t.Errorf("resolved to id %v, want 6", got)
	}
}

// TestStdoutStaysValidJSONWithProgressOnStderr is what lets an agent pipe npmctl into
// a parser while a human still sees progress.
func TestStdoutStaysValidJSONWithProgressOnStderr(t *testing.T) {
	h := newHarness(t)
	h.allowWrites()
	seedCRUD(h, crudCases[0])

	stdout, _, code := h.run("redirect", "update", "4", "--http-code", "302", "--yes")
	if code != exitcode.OK {
		t.Fatalf("want exit 0, got %d", code)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("stdout is not valid JSON despite progress output:\n%s\nerror: %v", stdout, err)
	}
}

// TestResolveByDomainAcrossCollections keeps the "never invent an ID" rule usable.
func TestResolveByDomainAcrossCollections(t *testing.T) {
	cases := []struct {
		group, domain string
		wantID        float64
	}{
		{"redirect", "old.example.com", 4},
		{"dead-host", "parked.example.com", 5},
	}
	for _, tc := range cases {
		t.Run(tc.group, func(t *testing.T) {
			h := newHarness(t)
			for _, c := range crudCases {
				if c.group == tc.group {
					seedCRUD(h, c)
				}
			}
			stdout, stderr, code := h.run(tc.group, "get", tc.domain)
			if code != exitcode.OK {
				t.Fatalf("want exit 0, got %d\n%s", code, stderr)
			}
			if got := decodeJSON(t, stdout)["id"]; got != tc.wantID {
				t.Errorf("resolved to id %v, want %v", got, tc.wantID)
			}
		})
	}
}

// TestUnknownDomainExitsFive keeps the not-found contract uniform.
func TestUnknownDomainExitsFive(t *testing.T) {
	h := newHarness(t)
	seedCRUD(h, crudCases[0])
	_, _, code := h.run("redirect", "get", "nope.example.com")
	if code != exitcode.NotFound {
		t.Fatalf("want exit %d, got %d", exitcode.NotFound, code)
	}
}
