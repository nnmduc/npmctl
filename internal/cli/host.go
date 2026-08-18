package cli

import (
	"context"
	"strconv"

	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

func newHostCommand(f *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "host",
		Aliases: []string{"proxy-host"},
		Short:   "Manage proxy hosts",
		Long: "Proxy hosts are NPM's reverse-proxy entries.\n\n" +
			"Every subcommand accepts either a numeric ID or a domain name, so you never\n" +
			"have to invent an ID. Writes require NPMCTL_ALLOW_WRITE=1 and --yes.",
	}
	cmd.AddCommand(
		newHostListCommand(f), newHostGetCommand(f), newHostCreateCommand(f),
		newHostUpdateCommand(f), newHostRemoveCommand(f), newHostToggleCommand(f, true), newHostToggleCommand(f, false),
	)
	return cmd
}

// resolveHost accepts an ID or a domain name. Resolving a name to an ID with a
// read is what lets the agent protocol forbid inventing IDs.

// resolveHost accepts an ID or a domain name. Resolving a name to an ID with a
// read is what lets the agent protocol forbid inventing IDs.
func resolveHost(ctx context.Context, c *npmapi.Client, ref string) (*npmapi.ProxyHost, error) {
	if id, err := strconv.Atoi(ref); err == nil {
		return c.GetProxyHost(ctx, id)
	}
	return c.FindProxyHostByDomain(ctx, ref)
}

func newHostListCommand(f *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List proxy hosts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := newRuntime(f)
			if err != nil {
				return err
			}
			c, err := rt.authenticated()
			if err != nil {
				return err
			}
			hosts, err := c.ListProxyHosts(cmd.Context())
			if err != nil {
				return err
			}
			return output.RenderWith(rt.stdout, rt.format, hostColumns, hosts)
		},
	}
}

func newHostGetCommand(f *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id|domain>",
		Short: "Show one proxy host",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(f)
			if err != nil {
				return err
			}
			c, err := rt.authenticated()
			if err != nil {
				return err
			}
			h, err := resolveHost(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			return output.Render(rt.stdout, rt.format, h)
		},
	}
}
