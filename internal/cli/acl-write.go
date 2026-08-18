// Access-list create and update, plus the item-level diff that makes a
// full-replacement update safe to review before it is applied.
package cli

import (
	"context"
	"fmt"

	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

func newACLCreateCommand(f *globalFlags) *cobra.Command {
	af := &aclFlags{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an access list",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			p, err := af.payload(cmd)
			if err != nil {
				return err
			}
			if err := requireFlags(map[string]bool{"--name": af.name != ""}); err != nil {
				return err
			}
			p.Set("name", af.name)
			body, err := p.Map()
			if err != nil {
				return err
			}

			var created *npmapi.AccessList
			op := Op{
				Verb:     "create",
				Kind:     "access-list",
				Resource: fmt.Sprintf("access-list %q", af.name),
				Method:   "POST",
				Path:     "/nginx/access-lists",
				Body:     body,
				Tier:     TierNormal,
			}
			if err := rt.gate().run(cmd.Context(), op, func(ctx context.Context) error {
				created, err = c.CreateAccessList(ctx, body)
				return err
			}); err != nil {
				return err
			}
			if created == nil {
				return nil
			}
			return output.Render(rt.stdout, rt.format, created)
		},
	}
	af.register(cmd)
	return cmd
}

func newACLUpdateCommand(f *globalFlags) *cobra.Command {
	af := &aclFlags{}
	cmd := &cobra.Command{
		Use:   "update <id|name>",
		Short: "Update an access list (items and clients are REPLACED, not merged)",
		Long: "Replaces the fields you pass. --item and --client replace their arrays entirely:\n" +
			"every user you want to keep must be listed with its password, because NPM never\n" +
			"returns existing passwords and npmctl will not invent them.\n\n" +
			"Run with --dry-run first to see the ADDED / REMOVED / PASSWORD RESET breakdown.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			target, err := resolveACL(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			p, err := af.payload(cmd)
			if err != nil {
				return err
			}
			body, err := p.Map()
			if err != nil {
				return err
			}

			// Report exactly which users change before anything is sent. A
			// full-replacement update is safe only if the operator can see what it
			// replaces.
			if items, ok := body["items"].([]npmapi.AccessListItem); ok {
				diff := diffACLItems(target.Items, items)
				fmt.Fprintf(rt.stderr, "access-list %q item changes:\n%s", target.Name, diff.render())
				if err := diff.refuseIfDestructive(); err != nil {
					return err
				}
			}

			op := Op{
				Verb:     "update",
				Kind:     "access-list",
				Resource: fmt.Sprintf("access-list %d (%s)", target.ID, target.Name),
				TargetID: target.ID,
				Method:   "PUT",
				Path:     fmt.Sprintf("/nginx/access-lists/%d", target.ID),
				Body:     body,
				Tier:     TierNormal,
				Fetch:    fetchACL(c, target.ID),
			}
			var updated *npmapi.AccessList
			if err := rt.gate().run(cmd.Context(), op, func(ctx context.Context) error {
				updated, err = c.UpdateAccessList(ctx, target.ID, body)
				return err
			}); err != nil {
				return err
			}
			if updated == nil {
				return nil
			}
			return output.Render(rt.stdout, rt.format, updated)
		},
	}
	af.register(cmd)
	return cmd
}

// aclItemDiff classifies what a replacement items array does to existing users.
