// The gate's individual steps: authorization, post-write nginx verification,
// pre-image capture and dry-run preview.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nnmduc/npmctl/internal/auth"
	"github.com/nnmduc/npmctl/internal/exitcode"
	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/nnmduc/npmctl/internal/undo"
)

// authorize enforces the two-factor write gate and the per-tier extras.
func (g *gate) authorize(op Op, dependents []string) error {
	f := g.rt.flags

	// Both factors are required. --yes alone is four characters an agent supplies
	// itself; NPMCTL_ALLOW_WRITE=1 is outside argv, so setting it is a separate
	// visible act and the host tool's permission prompt still fires.
	if !auth.WriteAllowed() && !f.yes {
		return exitcode.New(exitcode.Refused,
			"refusing to %s %s: writes require BOTH %s=1 in the environment and --yes",
			op.Verb, op.Resource, auth.EnvAllowWrite)
	}
	if !auth.WriteAllowed() {
		return exitcode.New(exitcode.Refused,
			"refusing to %s %s: --yes was given but %s=1 is not set in the environment",
			op.Verb, op.Resource, auth.EnvAllowWrite)
	}
	if !f.yes {
		return exitcode.New(exitcode.Refused,
			"refusing to %s %s: %s=1 is set but --yes was not given",
			op.Verb, op.Resource, auth.EnvAllowWrite)
	}

	if op.TouchesAdvancedConfig && !f.allowAdvancedConfg {
		return exitcode.New(exitcode.Refused,
			"refusing to write advanced_config on %s without --allow-advanced-config: "+
				"it accepts raw nginx directives, which can expose NPM's data volume over HTTP",
			op.Resource)
	}

	if op.Tier == TierDestructive && len(dependents) > 0 && !f.cascadeAck {
		return exitcode.New(exitcode.Refused,
			"refusing to %s %s: %d object(s) still reference it (%s) — pass --cascade-ack to proceed anyway",
			op.Verb, op.Resource, len(dependents), strings.Join(dependents, ", "))
	}

	// A Privileged operation, or any advanced_config write, requires a real
	// terminal. This is the one control that cannot be satisfied by an
	// environment variable an automated caller can set for itself.
	if op.Tier == TierPrivileged || op.TouchesAdvancedConfig {
		if err := g.confirmInteractively(op); err != nil {
			return err
		}
	}
	return nil
}

// verify re-reads the mutated object and fails with exit 8 when nginx did not
// reload. NPM records the failure in meta and still answers 200.

// verify re-reads the mutated object and fails with exit 8 when nginx did not
// reload. NPM records the failure in meta and still answers 200.
func (g *gate) verify(ctx context.Context, op Op) error {
	if op.Verify == nil {
		return nil
	}
	meta, err := op.Verify(ctx)
	if err != nil {
		// The write succeeded; only the confirmation read failed. Saying so is
		// more useful than reporting the whole operation as failed.
		fmt.Fprintf(g.rt.stderr,
			"warning: %s applied, but verifying nginx health failed: %v\n", op.Resource, err)
		return nil
	}
	online, present := meta.NginxOnline()
	if present && !online {
		errText := meta.NginxErr()
		if errText == "" {
			errText = "(nginx_err was empty)"
		}
		fmt.Fprintf(g.rt.stderr,
			"%s was applied, but nginx is NOT online. The site may be down.\nnginx_err: %s\n"+
				"Recover with: npmctl undo list -p %s\n",
			op.Resource, errText, g.rt.profileName)
		return exitcode.New(exitcode.NginxUnhealthy,
			"%s applied but nginx reload failed", op.Resource)
	}
	return nil
}

// capture appends the pre-image to the journal. The value is serialised RAW —
// the scrubber is not applied, because a redacted pre-image cannot be restored.

// capture appends the pre-image to the journal. The value is serialised RAW —
// the scrubber is not applied, because a redacted pre-image cannot be restored.
func (g *gate) capture(op Op, current any) (string, error) {
	raw, err := json.Marshal(current)
	if err != nil {
		return "", err
	}
	e := &undo.Entry{
		Profile:  g.rt.profileName,
		URL:      g.rt.profile.URL,
		Verb:     op.Verb,
		Resource: op.Resource,
		Kind:     op.Kind,
		TargetID: op.TargetID,
		Method:   op.Method,
		Path:     op.Path,
		PreImage: raw,
		Note:     op.Note,
	}
	return g.rt.journal.Append(e, time.Now())
}

// preview renders what would change and exits 0 with dry_run marked, so an agent
// keying on exit status cannot mistake a simulation for a completed write.

// preview renders what would change and exits 0 with dry_run marked, so an agent
// keying on exit status cannot mistake a simulation for a completed write.
func (g *gate) preview(op Op, current any, dependents []string) error {
	rt := g.rt
	fmt.Fprintf(rt.stderr, "DRY RUN — no mutating request will be sent\n")

	body := op.Body
	if p, ok := body.(*npmapi.Payload); ok {
		m, err := p.Map()
		if err != nil {
			return err
		}
		body = m
	}
	out := map[string]any{
		"dry_run":  true,
		"verb":     op.Verb,
		"resource": op.Resource,
		"tier":     op.Tier.String(),
		"method":   op.Method,
		"path":     op.Path,
	}
	if body != nil {
		out["body"] = body
	}
	if current != nil {
		out["current"] = current
	}
	if len(dependents) > 0 {
		out["dependents"] = dependents
		out["dependents_note"] = "a real run requires --cascade-ack"
	}
	if op.Note != "" {
		out["warning"] = op.Note
	}
	if op.TouchesAdvancedConfig {
		out["advanced_config_note"] = "a real run requires --allow-advanced-config and a terminal"
	}
	return output.Render(rt.stdout, rt.format, out)
}
