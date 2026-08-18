package cli

import (
	"context"
	"fmt"

	"github.com/nnmduc/npmctl/internal/exitcode"
	"github.com/nnmduc/npmctl/internal/npmapi"
)

// Tier grades how much confirmation a mutation needs.
type Tier int

const (
	// TierNormal covers create, update, enable and disable.
	TierNormal Tier = iota
	// TierDestructive covers deletes: they additionally require a dependency
	// scan and --cascade-ack when other objects reference the target.
	TierDestructive
	// TierPrivileged covers operations whose blast radius is the instance
	// itself. It refuses to run without a terminal, so no unattended process
	// can reach it.
	TierPrivileged
)

func (t Tier) String() string {
	switch t {
	case TierDestructive:
		return "destructive"
	case TierPrivileged:
		return "privileged"
	default:
		return "normal"
	}
}

// Op describes one mutation. Commands never call a write method directly; they
// describe it here and hand it to the gate.

// Op describes one mutation. Commands never call a write method directly; they
// describe it here and hand it to the gate.
type Op struct {
	Verb     string // create|update|delete|enable|disable|restore
	Kind     string // "proxy-host"
	Resource string // human label, e.g. "proxy-host 12 (app.example.com)"
	TargetID int
	Method   string
	Path     string
	Body     any
	Tier     Tier

	// Fetch returns the object as it exists now, plus its modified_on. It is
	// called during preview AND again immediately before the write, which is what
	// makes compare-and-swap possible on an API that offers no ETag.
	Fetch func(ctx context.Context) (any, string, error)

	// Dependents names objects that reference the target. Required for deletes.
	Dependents func(ctx context.Context) ([]string, error)

	// Verify re-reads the object after the write and reports nginx health. NPM
	// answers 200 for a write whose nginx reload failed, so without this the tool
	// would report success while the site is down.
	Verify func(ctx context.Context) (npmapi.Meta, error)

	// TouchesAdvancedConfig marks a write carrying raw nginx directives.
	TouchesAdvancedConfig bool

	// Note is an irreversibility caveat shown in the preview and stored in the
	// journal, e.g. that ACME revocation cannot be undone.
	Note string
}

// gate executes mutations under the write gate.

// gate executes mutations under the write gate.
type gate struct{ rt *runtime }

func (r *runtime) gate() *gate { return &gate{rt: r} }

// run performs the whole transaction in order: preview, dependency scan,
// authorize, compare-and-swap, capture, execute, verify.

// run performs the whole transaction in order: preview, dependency scan,
// authorize, compare-and-swap, capture, execute, verify.
func (g *gate) run(ctx context.Context, op Op, do func(ctx context.Context) error) error {
	rt := g.rt
	f := rt.flags

	// Step 1 — resolve and preview. Reads are permitted here even under
	// --dry-run: the invariant is "no MUTATING request", not "no request". A
	// preview that cannot name what it is about to delete is not a preview.
	var current any
	var modifiedOn string
	if op.Fetch != nil {
		c, m, err := op.Fetch(ctx)
		if err != nil {
			return err
		}
		current, modifiedOn = c, m
	}

	// Step 2 — dependency scan, before any delete.
	var dependents []string
	if op.Dependents != nil {
		d, err := op.Dependents(ctx)
		if err != nil {
			return err
		}
		dependents = d
	}

	if f.dryRun {
		return g.preview(op, current, dependents)
	}

	// Step 3 — authorize.
	if err := g.authorize(op, dependents); err != nil {
		return err
	}

	// Step 4 — compare-and-swap. Re-read immediately before writing: the window
	// between preview and confirmation is exactly when a concurrent edit lands,
	// and --dry-run followed by a human typing --yes widens it further.
	if op.Fetch != nil && modifiedOn != "" {
		_, now, err := op.Fetch(ctx)
		if err != nil {
			return err
		}
		if now != modifiedOn {
			return exitcode.New(exitcode.Refused,
				"%s changed since it was previewed (modified_on %s -> %s) — re-run to see the current state",
				op.Resource, modifiedOn, now)
		}
	}

	// Step 5 — capture the pre-image BEFORE executing.
	if current != nil {
		path, err := g.capture(op, current)
		if err != nil {
			return exitcode.Wrap(exitcode.Generic, err, "capture undo pre-image")
		}
		fmt.Fprintf(rt.stderr, "undo pre-image: %s\n", path)
	}

	// Step 6 — execute.
	if err := do(ctx); err != nil {
		return err
	}

	// Step 7 — verify nginx actually reloaded.
	return g.verify(ctx, op)
}

// authorize enforces the two-factor write gate and the per-tier extras.
