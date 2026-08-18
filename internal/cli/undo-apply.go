// `undo apply` — replaying a captured pre-image. This is a mutation and receives
// no exemption: it routes through the same gate as any other write.
package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nnmduc/npmctl/internal/exitcode"
	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/nnmduc/npmctl/internal/undo"
	"github.com/spf13/cobra"
)

func newUndoApplyCommand(f *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "apply <entry>",
		Short: "Replay a pre-image as a write (gated like any other mutation)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(f)
			if err != nil {
				return err
			}
			e, err := loadEntry(rt, args[0])
			if err != nil {
				return err
			}
			c, err := rt.authenticated()
			if err != nil {
				return err
			}
			body, err := e.Body()
			if err != nil {
				var unknown *undo.UnknownKeyError
				if errors.As(err, &unknown) {
					// Refuse rather than let the API reject it: a 400 here would
					// not explain that the entry predates a schema change.
					return exitcode.Wrap(exitcode.Refused, err, "cannot replay entry %s", e.ID)
				}
				return exitcode.Wrap(exitcode.Refused, err, "cannot replay entry %s", e.ID)
			}
			if e.Kind != "proxy-host" {
				return exitcode.New(exitcode.Refused,
					"restoring %s is not supported in v1 — inspect the pre-image with `npmctl undo show %s`", e.Kind, e.ID)
			}
			return applyProxyHostRestore(cmd.Context(), rt, c, e, body)
		},
	}
}

// applyProxyHostRestore replays a proxy-host pre-image through the gate, so the
// restore inherits the write gate, compare-and-swap, its own pre-image capture,
// and post-write nginx verification.

// applyProxyHostRestore replays a proxy-host pre-image through the gate, so the
// restore inherits the write gate, compare-and-swap, its own pre-image capture,
// and post-write nginx verification.
func applyProxyHostRestore(ctx context.Context, rt *runtime, c *npmapi.Client, e *undo.Entry, body map[string]any) error {
	// A restore only makes sense against a resource that still exists. Recreating
	// a deleted one would mint a different ID and silently diverge from the
	// pre-image, so it is refused with the command that would do it deliberately.
	current, err := c.GetProxyHost(ctx, e.TargetID)
	if err != nil {
		if npmapi.IsNotFound(err) {
			return exitcode.New(exitcode.NotFound,
				"proxy-host %d no longer exists, so there is nothing to restore in place.\n"+
					"Recreate it explicitly with:\n  npmctl host create %s",
				e.TargetID, createFlagsFromBody(body))
		}
		return err
	}

	note := e.Note
	if _, ok := body["advanced_config"]; ok && body["advanced_config"] != "" {
		fmt.Fprintf(rt.stderr, "advanced_config diff for restore:\n%s\n",
			diffLines(current.AdvancedConfig, fmt.Sprint(body["advanced_config"])))
	}

	op := Op{
		Verb:     "restore",
		Kind:     "proxy-host",
		Resource: fmt.Sprintf("proxy-host %d (%s) from entry %s", current.ID, current.Name(), e.ID),
		TargetID: current.ID,
		Method:   "PUT",
		Path:     fmt.Sprintf("/nginx/proxy-hosts/%d", current.ID),
		Body:     body,
		// Privileged: a restore rewrites many fields at once from a file the
		// operator has probably not read, so it needs a human at a terminal.
		Tier:                  TierPrivileged,
		Fetch:                 fetchProxyHost(c, current.ID),
		Verify:                verifyProxyHost(c, current.ID),
		TouchesAdvancedConfig: bodyHasAdvancedConfig(body, current.AdvancedConfig),
		Note:                  note,
	}
	var restored *npmapi.ProxyHost
	if err := rt.gate().run(ctx, op, func(ctx context.Context) error {
		restored, err = c.UpdateProxyHost(ctx, current.ID, body)
		return err
	}); err != nil {
		return err
	}
	if restored == nil {
		return nil
	}
	return output.Render(rt.stdout, rt.format, restored)
}

// bodyHasAdvancedConfig reports whether the restore would change raw nginx
// directives, which needs the separate advanced-config gate.

// bodyHasAdvancedConfig reports whether the restore would change raw nginx
// directives, which needs the separate advanced-config gate.
func bodyHasAdvancedConfig(body map[string]any, current string) bool {
	v, ok := body["advanced_config"]
	if !ok {
		return false
	}
	return fmt.Sprint(v) != current
}

// createFlagsFromBody renders the equivalent `host create` flags, so a refusal
// hands over the exact next command instead of a JSON blob.

// createFlagsFromBody renders the equivalent `host create` flags, so a refusal
// hands over the exact next command instead of a JSON blob.
func createFlagsFromBody(body map[string]any) string {
	var parts []string
	if domains, ok := body["domain_names"].([]any); ok {
		for _, d := range domains {
			parts = append(parts, fmt.Sprintf("--domain %v", d))
		}
	}
	for flag, key := range map[string]string{
		"--forward-scheme": "forward_scheme",
		"--forward-host":   "forward_host",
		"--forward-port":   "forward_port",
	} {
		if v, ok := body[key]; ok {
			parts = append(parts, fmt.Sprintf("%s %v", flag, v))
		}
	}
	return strings.Join(parts, " ")
}
