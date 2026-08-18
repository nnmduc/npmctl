// Access-list reads and delete.
package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

// resolveACL accepts a numeric ID or a name.
func resolveACL(ctx context.Context, c *npmapi.Client, ref string) (*npmapi.AccessList, error) {
	if id, err := strconv.Atoi(ref); err == nil {
		return c.GetAccessList(ctx, id, "items", "clients", "proxy_hosts")
	}
	return c.FindAccessListByName(ctx, ref)
}

var aclColumns = []output.Column{
	{Header: "ID", Key: "id"},
	{Header: "NAME", Key: "name"},
	{Header: "SATISFY", Key: "satisfy_any"},
	{Header: "PASS-AUTH", Key: "pass_auth"},
	{Header: "HOSTS", Key: "proxy_host_count"},
}

func newACLListCommand(f *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List access lists",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			lists, err := c.ListAccessLists(cmd.Context(), "items", "clients")
			if err != nil {
				return err
			}
			return output.RenderWith(rt.stdout, rt.format, aclColumns, lists)
		},
	}
}

func newACLGetCommand(f *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id|name>",
		Short: "Show one access list",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			list, err := resolveACL(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			return output.Render(rt.stdout, rt.format, list)
		},
	}
}

func newACLRemoveCommand(f *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "rm <id|name>",
		Aliases: []string{"delete"},
		Short:   "Delete an access list",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			list, err := resolveACL(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			op := Op{
				Verb:     "delete",
				Kind:     "access-list",
				Resource: fmt.Sprintf("access-list %d (%s)", list.ID, list.Name),
				TargetID: list.ID,
				Method:   "DELETE",
				Path:     fmt.Sprintf("/nginx/access-lists/%d", list.ID),
				Tier:     TierDestructive,
				Fetch:    fetchACL(c, list.ID),
				// Hosts referencing this list lose their access control on the next
				// nginx reload, so the count is the dependency scan.
				Dependents: func(context.Context) ([]string, error) {
					if list.ProxyHostCount == 0 {
						return nil, nil
					}
					return []string{fmt.Sprintf("%d proxy host(s) use this access list", list.ProxyHostCount)}, nil
				},
			}
			if err := rt.gate().run(cmd.Context(), op, func(ctx context.Context) error {
				return c.DeleteAccessList(ctx, list.ID)
			}); err != nil {
				return err
			}
			if f.dryRun {
				return nil
			}
			return output.Render(rt.stdout, rt.format, map[string]any{
				"status": "deleted", "id": list.ID, "name": list.Name,
			})
		},
	}
}

func fetchACL(c *npmapi.Client, id int) func(context.Context) (any, string, error) {
	return func(ctx context.Context) (any, string, error) {
		l, err := c.GetAccessList(ctx, id, "items", "clients")
		if err != nil {
			return nil, "", err
		}
		return l, l.ModifiedOn, nil
	}
}
