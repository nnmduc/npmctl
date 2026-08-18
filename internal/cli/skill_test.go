package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nnmduc/npmctl/internal/exitcode"
	"github.com/nnmduc/npmctl/internal/skill"
	"gopkg.in/yaml.v3"
)

// skillFrontmatter is the subset of the Agent Skills spec we assert on.
type skillFrontmatter struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	License      string   `yaml:"license"`
	AllowedTools []string `yaml:"allowed-tools"`
}

// parseFrontmatter splits the YAML frontmatter from SKILL.md.
func parseFrontmatter(t *testing.T) (skillFrontmatter, string) {
	t.Helper()
	raw, err := skill.SkillMarkdown()
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	if !strings.HasPrefix(content, "---\n") {
		t.Fatal("SKILL.md must begin with YAML frontmatter")
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		t.Fatal("SKILL.md frontmatter is not terminated")
	}
	var fm skillFrontmatter
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		t.Fatalf("frontmatter is not valid YAML: %v", err)
	}
	return fm, rest[end+len("\n---\n"):]
}

// TestSkillFrontmatterMatchesSpec covers the spec's required properties, including the
// rule that `name` must match the containing directory.
func TestSkillFrontmatterMatchesSpec(t *testing.T) {
	fm, body := parseFrontmatter(t)

	if fm.Name != skill.Name {
		t.Errorf("frontmatter name = %q, but the skill installs into %q; the spec requires them to match", fm.Name, skill.Name)
	}
	// Lowercase alphanumeric plus hyphen.
	for _, r := range fm.Name {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			t.Errorf("name %q contains %q, which the spec disallows", fm.Name, r)
		}
	}
	if strings.TrimSpace(fm.Description) == "" {
		t.Error("description is required")
	}
	if !strings.Contains(strings.ToLower(fm.Description), "npm") &&
		!strings.Contains(strings.ToLower(fm.Description), "nginx") {
		t.Error("the description should say what the skill manages, so an agent can route to it")
	}
	if strings.TrimSpace(body) == "" {
		t.Error("SKILL.md has no body")
	}
}

// TestNoWriteCommandIsPreApproved is the load-bearing assertion of the whole phase.
//
// The original design granted Bash(npmctl:*), which pre-approved every command and
// suppressed the tool-permission prompt — the only checkpoint a human actually sees. The
// grant is now read verbs and --dry-run only.
func TestNoWriteCommandIsPreApproved(t *testing.T) {
	fm, _ := parseFrontmatter(t)
	if len(fm.AllowedTools) == 0 {
		t.Fatal("allowed-tools is empty; expected an explicit read-only grant")
	}

	// Verbs that mutate state. None may appear in the grant.
	writeVerbs := []string{"create", "update", "rm", "delete", "enable", "disable",
		"renew", "upload", "apply", "login", "logout", "set"}

	for _, entry := range fm.AllowedTools {
		// A --dry-run grant is safe by construction: it issues no mutating request.
		if strings.Contains(entry, "--dry-run") {
			continue
		}
		// Compare whole words: a substring test would flag `settings list`, whose first
		// three letters happen to spell "set".
		for _, word := range grantWords(entry) {
			for _, verb := range writeVerbs {
				if word == verb {
					t.Errorf("allowed-tools pre-approves a write: %q (verb %q)", entry, verb)
				}
			}
		}
		// A wildcard over the whole binary would re-introduce the original flaw.
		if entry == "Bash(npmctl:*)" || entry == "Bash(npmctl *:*)" {
			t.Errorf("%q pre-approves the entire surface, defeating the permission prompt", entry)
		}
	}
}

// TestUndoApplyIsNotPreApprovedButReadsAre: undo list/show are reads; apply is a write.
func TestUndoApplyIsNotPreApprovedButReadsAre(t *testing.T) {
	fm, _ := parseFrontmatter(t)
	joined := strings.Join(fm.AllowedTools, "\n")

	for _, want := range []string{"undo list", "undo show"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%q is a read and should be pre-approved", want)
		}
	}
	if strings.Contains(joined, "undo apply") {
		t.Error("`undo apply` is a write and must not be pre-approved")
	}
}

// TestDeferredCommandsAbsentFromGrant cross-checks the scope decision at the grant level
// too, even though phase 3 already asserts they are absent from the binary.
func TestDeferredCommandsAbsentFromGrant(t *testing.T) {
	fm, _ := parseFrontmatter(t)
	joined := strings.Join(fm.AllowedTools, "\n")
	for _, banned := range []string{"user", "login-as", "settings set"} {
		if strings.Contains(joined, "npmctl "+banned) {
			t.Errorf("allowed-tools references the deferred command %q", banned)
		}
	}
}

// TestSkillBodyCarriesTheSafetyProtocol keeps the rules that matter from being edited away.
func TestSkillBodyCarriesTheSafetyProtocol(t *testing.T) {
	_, body := parseFrontmatter(t)
	required := map[string]string{
		"NPMCTL_ALLOW_WRITE": "the out-of-argv write factor",
		"--dry-run":          "the preview instruction",
		"Exit 3":             "the refusal rule",
		"Exit 9":             "the re-authentication rule",
		"Exit 8":             "the nginx-unhealthy rule",
		"advanced_config":    "the raw-nginx prohibition",
		"revoke":             "the certificate revocation warning",
		"undo":               "the recovery instruction",
	}
	for needle, why := range required {
		if !strings.Contains(body, needle) {
			t.Errorf("SKILL.md body is missing %s (looked for %q)", why, needle)
		}
	}
	// The honesty requirement: the skill must not overclaim.
	if !strings.Contains(strings.ToLower(body), "not a determined attacker") {
		t.Error("SKILL.md must state the honest limit of the gates")
	}
}

// TestGeneratedReferenceCoversEveryCommand: the reference is generated so it cannot drift
// from the binary, and this proves the generator actually walks the whole tree.
func TestGeneratedReferenceCoversEveryCommand(t *testing.T) {
	root, _ := NewRootCommand()
	doc := renderCommandReference(root)

	for _, path := range commandPaths(t) {
		if !strings.Contains(doc, "`"+path+"`") {
			t.Errorf("generated reference omits the command %q", path)
		}
	}
	// Exit codes and tiers are part of the contract agents read.
	for _, want := range []string{"Exit codes", "Write gate", "destructive", "privileged",
		"NPMCTL_ALLOW_WRITE", "applied, but the nginx reload failed"} {
		if !strings.Contains(doc, want) {
			t.Errorf("generated reference is missing %q", want)
		}
	}
}

// TestGeneratedReferenceMarksWritesWithTiers keeps the tier table honest.
func TestGeneratedReferenceMarksWritesWithTiers(t *testing.T) {
	root, _ := NewRootCommand()
	doc := renderCommandReference(root)

	cases := map[string]string{
		"cert rm":     "destructive",
		"host rm":     "destructive",
		"undo apply":  "privileged",
		"host create": "normal",
	}
	for path, tier := range cases {
		// Anchor on the HEADING, not the first mention: a command name also appears
		// inside its group's prose.
		heading := "# `" + path + "`\n"
		idx := strings.Index(doc, heading)
		if idx < 0 {
			t.Errorf("reference omits the heading for %q", path)
			continue
		}
		section := doc[idx+len(heading):]
		if end := strings.Index(section, "\n#"); end > 0 {
			section = section[:end]
		}
		if !strings.Contains(section, tier) {
			t.Errorf("%q should be marked tier %q in the reference", path, tier)
		}
	}
}

// grantWords splits an allowed-tools entry into comparable words, e.g.
// "Bash(npmctl settings list:*)" -> ["npmctl", "settings", "list"].
func grantWords(entry string) []string {
	inner := entry
	if i := strings.Index(inner, "("); i >= 0 {
		inner = inner[i+1:]
	}
	inner = strings.TrimSuffix(inner, ")")
	inner = strings.ReplaceAll(inner, ":*", " ")
	inner = strings.ReplaceAll(inner, "*", " ")
	return strings.Fields(inner)
}

// TestSkillInstallIsIdempotent covers the manifest behaviour end to end.
func TestSkillInstallIsIdempotent(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()

	stdout, _, code := h.run("skill", "install", "--dir", dir)
	if code != exitcode.OK {
		t.Fatalf("first install failed with %d", code)
	}
	if !strings.Contains(stdout, "created") {
		t.Errorf("first install should report created files:\n%s", stdout)
	}

	stdout, _, code = h.run("skill", "install", "--dir", dir)
	if code != exitcode.OK {
		t.Fatalf("second install failed with %d", code)
	}
	if strings.Contains(stdout, "created") {
		t.Errorf("second install should not re-create files:\n%s", stdout)
	}
	if !strings.Contains(stdout, "unchanged") {
		t.Errorf("second install should report files as unchanged:\n%s", stdout)
	}
}

// TestSkillInstallPreservesLocalEdits resolves the original contradiction between
// "updates in place" and "warn before overwriting": an unchanged file is replaced
// silently, a locally modified one is reported and left alone.
func TestSkillInstallPreservesLocalEdits(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()

	if _, _, code := h.run("skill", "install", "--dir", dir); code != exitcode.OK {
		t.Fatalf("first install failed with %d", code)
	}
	edited := filepath.Join(dir, skill.Name, "SKILL.md")
	const marker = "\n<!-- operator's local note -->\n"
	original, err := os.ReadFile(edited)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(edited, append(original, []byte(marker)...), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := h.run("skill", "install", "--dir", dir)
	if code != exitcode.OK {
		t.Fatalf("reinstall failed with %d", code)
	}
	if !strings.Contains(stdout, "preserved") {
		t.Errorf("the modified file should be reported as preserved:\n%s", stdout)
	}
	if !strings.Contains(stderr, "local edits") {
		t.Errorf("stderr should tell the operator local edits were kept:\n%s", stderr)
	}
	after, err := os.ReadFile(edited)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), marker) {
		t.Error("the local edit was clobbered")
	}
}

// TestAgentsMDDedupesOnMarker: keying on a stable marker means rewording the block does
// not append a second copy.
func TestAgentsMDDedupesOnMarker(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	agents := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(agents, []byte("# Project agents\n\nExisting content.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Without --force the file is not touched: it is probably git-tracked.
	_, stderr, code := h.run("skill", "install", "--dir", dir, "--agents-md", agents)
	if code != exitcode.Refused {
		t.Fatalf("want exit %d without --force, got %d\n%s", exitcode.Refused, code, stderr)
	}
	if body, _ := os.ReadFile(agents); strings.Contains(string(body), agentsMarker) {
		t.Fatal("AGENTS.md was modified without --force")
	}

	if _, _, code := h.run("skill", "install", "--dir", dir, "--agents-md", agents, "--force"); code != exitcode.OK {
		t.Fatalf("install with --force failed with %d", code)
	}
	body, _ := os.ReadFile(agents)
	if strings.Count(string(body), agentsMarker) != 1 {
		t.Fatalf("expected exactly one marker, got %d", strings.Count(string(body), agentsMarker))
	}
	if !strings.Contains(string(body), "Existing content.") {
		t.Error("existing AGENTS.md content was lost")
	}

	// A second run must not append again.
	if _, _, code := h.run("skill", "install", "--dir", dir, "--agents-md", agents, "--force"); code != exitcode.OK {
		t.Fatalf("second install failed with %d", code)
	}
	body, _ = os.ReadFile(agents)
	if got := strings.Count(string(body), agentsMarker); got != 1 {
		t.Errorf("marker appears %d times after a second run, want 1", got)
	}
}

// TestSkillInstallDryRunWritesNothing keeps --dry-run meaningful for this command too.
func TestSkillInstallDryRunWritesNothing(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()

	if _, _, code := h.run("skill", "install", "--dir", dir, "--dry-run"); code != exitcode.OK {
		t.Fatalf("dry run failed with %d", code)
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) != 0 {
		t.Errorf("--dry-run created files: %v", entries)
	}
}
