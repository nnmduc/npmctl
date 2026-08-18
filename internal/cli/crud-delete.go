// Generated delete and enable/disable commands for the shared CRUD shape.
package cli

import (
	"context"
	"fmt"

	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

func newCRUDRemoveCommand[T any](f *globalFlags, spec crudSpec[T]) *cobra.Command {
	return &cobra.Command{
		Use:     "rm " + spec.refHelp,
		Aliases: []string{"delete"},
		Short:   "Delete a " + spec.kind,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			target, err := spec.resolve(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			id := spec.idOf(target)

			op := gateOp(spec, c, "delete", id, label(spec, target))
			op.Method, op.Path = "DELETE", fmt.Sprintf("%s/%d", spec.path, id)
			op.Tier = TierDestructive
			if spec.deleteTier != TierNormal {
				op.Tier = spec.deleteTier
			}
			op.Note = spec.deleteNote
			// A delete never verifies nginx health on an object that no longer exists.
			op.Verify = nil
			if spec.dependents != nil {
				op.Dependents = func(ctx context.Context) ([]string, error) {
					return spec.dependents(ctx, c, target)
				}
			}

			if err := rt.gate().run(cmd.Context(), op, func(ctx context.Context) error {
				return spec.remove(ctx, c, id)
			}); err != nil {
				return err
			}
			if f.dryRun {
				return nil
			}
			return output.Render(rt.stdout, rt.format, map[string]any{
				"status": "deleted", "kind": spec.kind, "id": id, "resource": label(spec, target),
			})
		},
	}
}

func newCRUDToggleCommand[T any](f *globalFlags, spec crudSpec[T], enable bool) *cobra.Command {
	verb := "disable"
	if enable {
		verb = "enable"
	}
	return &cobra.Command{
		Use:   verb + " " + spec.refHelp,
		Short: verb + " a " + spec.kind,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			target, err := spec.resolve(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			id := spec.idOf(target)

			op := gateOp(spec, c, verb, id, label(spec, target))
			op.Method, op.Path = "POST", fmt.Sprintf("%s/%d/%s", spec.path, id, verb)

			if err := rt.gate().run(cmd.Context(), op, func(ctx context.Context) error {
				return spec.setEnabled(ctx, c, id, enable)
			}); err != nil {
				return err
			}
			if f.dryRun {
				return nil
			}
			return output.Render(rt.stdout, rt.format, map[string]any{"status": verb + "d", "id": id})
		},
	}
}

// label renders "<kind> <id> (<name>)" for previews, confirmations and journal entries.

// label renders "<kind> <id> (<name>)" for previews, confirmations and journal entries.
func label[T any](spec crudSpec[T], target *T) string {
	name := ""
	if spec.nameOf != nil {
		name = spec.nameOf(target)
	}
	if name == "" {
		return fmt.Sprintf("%s %d", spec.kind, spec.idOf(target))
	}
	return fmt.Sprintf("%s %d (%s)", spec.kind, spec.idOf(target), name)
}
