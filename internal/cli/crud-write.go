// Generated write commands for the shared CRUD shape. Every one routes through the
// gate; none calls an npmapi write method directly.
package cli

import (
	"context"
	"fmt"

	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

// gateOp assembles the Op for a resource mutation, wiring the preview/CAS read and
// the post-write nginx health check from the spec's accessors.
func gateOp[T any](spec crudSpec[T], c *npmapi.Client, verb string, id int, label string) Op {
	op := Op{
		Verb:     verb,
		Kind:     spec.kind,
		Resource: label,
		TargetID: id,
	}
	if spec.get != nil && id > 0 {
		op.Fetch = func(ctx context.Context) (any, string, error) {
			cur, err := spec.get(ctx, c, id)
			if err != nil {
				return nil, "", err
			}
			return cur, spec.modifiedOf(cur), nil
		}
		if spec.metaOf != nil {
			op.Verify = func(ctx context.Context) (npmapi.Meta, error) {
				fresh, err := spec.get(ctx, c, id)
				if err != nil {
					return nil, err
				}
				return spec.metaOf(fresh), nil
			}
		}
	}
	return op
}

func newCRUDCreateCommand[T any](f *globalFlags, spec crudSpec[T]) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a " + spec.kind,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			p, err := spec.createPayload(cmd)
			if err != nil {
				return err
			}
			body, err := p.Map()
			if err != nil {
				return err
			}

			var created *T
			op := gateOp(spec, c, "create", 0, spec.kind)
			op.Method, op.Path, op.Body = "POST", spec.path, body
			op.TouchesAdvancedConfig = cmd.Flags().Changed("advanced-config")
			// A create that orders a certificate inline blocks on ACME.
			client := certTimeoutClient(c, body)
			op.Verify = func(ctx context.Context) (npmapi.Meta, error) {
				if created == nil || spec.metaOf == nil {
					return nil, nil
				}
				fresh, err := spec.get(ctx, c, spec.idOf(created))
				if err != nil {
					return nil, err
				}
				return spec.metaOf(fresh), nil
			}

			if err := rt.gate().run(cmd.Context(), op, func(ctx context.Context) error {
				created, err = spec.create(ctx, client, body)
				return err
			}); err != nil {
				return err
			}
			if created == nil {
				return nil
			}
			warnTLSCoercion(rt.stderr, body, created)
			return output.Render(rt.stdout, rt.format, created)
		},
	}
	spec.registerFlags(cmd)
	return cmd
}

func newCRUDUpdateCommand[T any](f *globalFlags, spec crudSpec[T]) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update " + spec.refHelp,
		Short: "Update a " + spec.kind + " (sends only the fields you pass)",
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
			p, err := spec.updatePayload(cmd)
			if err != nil {
				return err
			}
			body, err := p.Map()
			if err != nil {
				return err
			}
			id := spec.idOf(target)

			op := gateOp(spec, c, "update", id, label(spec, target))
			op.Method, op.Path, op.Body = "PUT", fmt.Sprintf("%s/%d", spec.path, id), body

			// Render a line diff before a raw nginx change is accepted.
			if newCfg, ok := body["advanced_config"]; ok && spec.advancedConfigOf != nil {
				current := spec.advancedConfigOf(target)
				if fmt.Sprint(newCfg) != current {
					op.TouchesAdvancedConfig = true
					fmt.Fprintf(rt.stderr, "advanced_config diff for %s:\n%s\n",
						label(spec, target), diffLines(current, fmt.Sprint(newCfg)))
				}
			}
			client := certTimeoutClient(c, body)

			var updated *T
			if err := rt.gate().run(cmd.Context(), op, func(ctx context.Context) error {
				updated, err = spec.update(ctx, client, id, body)
				return err
			}); err != nil {
				return err
			}
			if updated == nil {
				return nil
			}
			warnTLSCoercion(rt.stderr, body, updated)
			return output.Render(rt.stdout, rt.format, updated)
		},
	}
	spec.registerFlags(cmd)
	return cmd
}
