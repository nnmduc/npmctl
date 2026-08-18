package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// walk returns every command path in the tree, e.g. "host update".
func walk(c *cobra.Command, prefix string, out *[]string) {
	for _, sub := range c.Commands() {
		if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
			continue
		}
		path := strings.TrimSpace(prefix + " " + sub.Name())
		*out = append(*out, path)
		walk(sub, path, out)
	}
}

func commandPaths(t *testing.T) []string {
	t.Helper()
	root, _ := NewRootCommand()
	var paths []string
	walk(root, "", &paths)
	return paths
}

// deferredCommands are the v1 exclusions. Scope is a stronger control than
// tiering: a command that does not exist cannot be mis-gated, mis-tiered, or
// reached by an agent that ignores its instructions — nor by prompt injection
// through a hostname or advanced_config field.
var deferredCommands = []string{
	"user",         // the whole /users surface
	"login-as",     // POST /users/{id}/login returns an unrevocable ~1d JWT
	"set-password", // PUT /users/{id}/auth can seize the admin account
	"2fa",          // /users/{id}/2fa family
}

// TestDeferredCommandsAreAbsentFromBinary asserts the v1 scope boundary at the
// binary level, which is stronger than merely omitting them from the skill grant.
func TestDeferredCommandsAreAbsentFromBinary(t *testing.T) {
	paths := commandPaths(t)
	for _, banned := range deferredCommands {
		for _, path := range paths {
			for _, word := range strings.Fields(path) {
				if word == banned {
					t.Errorf("deferred command %q is present in the binary as %q", banned, path)
				}
			}
		}
	}
}

// TestSettingsSetIsAbsent guards PUT /settings/{id} specifically: free-form,
// version-dependent setting IDs with no safe default validation.
func TestSettingsSetIsAbsent(t *testing.T) {
	for _, path := range commandPaths(t) {
		if path == "settings set" {
			t.Error("`settings set` (PUT /settings/{id}) is deferred to v2 but exists in the binary")
		}
	}
}

// TestWriteGateFlagsExist pins the two-factor gate's argv half.
func TestWriteGateFlagsExist(t *testing.T) {
	root, _ := NewRootCommand()
	for _, name := range []string{"yes", "dry-run", "cascade-ack", "allow-advanced-config"} {
		if root.PersistentFlags().Lookup(name) == nil {
			t.Errorf("global flag --%s is missing", name)
		}
	}
}

// TestExpectedCommandsExist keeps the phase's functional surface honest.
func TestExpectedCommandsExist(t *testing.T) {
	want := []string{
		"auth login", "auth logout", "auth status", "auth whoami",
		"host list", "host get", "host create", "host update", "host rm", "host enable", "host disable",
		"undo list", "undo show", "undo apply",
		"version", "health",
	}
	have := map[string]bool{}
	for _, p := range commandPaths(t) {
		have[p] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("expected command %q is missing from the binary", w)
		}
	}
}

// TestNoCommandLacksAShortDescription keeps `--help` self-describing, which the
// agent protocol relies on for discovery.
func TestNoCommandLacksAShortDescription(t *testing.T) {
	root, _ := NewRootCommand()
	var check func(c *cobra.Command, prefix string)
	check = func(c *cobra.Command, prefix string) {
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
				continue
			}
			path := strings.TrimSpace(prefix + " " + sub.Name())
			if strings.TrimSpace(sub.Short) == "" {
				t.Errorf("command %q has no Short description", path)
			}
			check(sub, path)
		}
	}
	check(root, "")
}
