// Proxy-host mutations. Every command here routes through the write gate; none
// calls an npmapi write method directly.
package cli

import (
	"context"
	"fmt"

	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

func newHostUpdateCommand(f *globalFlags) *cobra.Command {
	hf := &hostFlags{}
	cmd := &cobra.Command{
		Use:   "update <id|domain>",
		Short: "Update a proxy host (sends only the fields you pass)",
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
			target, err := resolveHost(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			p, err := hf.payload(cmd)
			if err != nil {
				return err
			}
			body, err := p.Map()
			if err != nil {
				return err
			}
			client := certTimeoutClient(c, body)

			// Show the operator what a raw nginx change actually does.
			if cmd.Flags().Changed("advanced-config") {
				fmt.Fprintf(rt.stderr, "advanced_config diff for %s:\n%s\n",
					target.Name(), diffLines(target.AdvancedConfig, hf.advancedConfig))
			}

			op := Op{
				Verb:                  "update",
				Kind:                  "proxy-host",
				Resource:              fmt.Sprintf("proxy-host %d (%s)", target.ID, target.Name()),
				TargetID:              target.ID,
				Method:                "PUT",
				Path:                  fmt.Sprintf("/nginx/proxy-hosts/%d", target.ID),
				Body:                  body,
				Tier:                  TierNormal,
				TouchesAdvancedConfig: cmd.Flags().Changed("advanced-config"),
				Fetch:                 fetchProxyHost(c, target.ID),
				Verify:                verifyProxyHost(c, target.ID),
			}
			var updated *npmapi.ProxyHost
			if err := rt.gate().run(cmd.Context(), op, func(ctx context.Context) error {
				updated, err = client.UpdateProxyHost(ctx, target.ID, body)
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
	hf.register(cmd)
	return cmd
}

// fetchProxyHost supplies the gate's preview and compare-and-swap reads.

// fetchProxyHost supplies the gate's preview and compare-and-swap reads.

// fetchProxyHost supplies the gate's preview and compare-and-swap reads.
func fetchProxyHost(c *npmapi.Client, id int) func(context.Context) (any, string, error) {
	return func(ctx context.Context) (any, string, error) {
		h, err := c.GetProxyHost(ctx, id)
		if err != nil {
			return nil, "", err
		}
		return h, h.ModifiedOn, nil
	}
}

func verifyProxyHost(c *npmapi.Client, id int) func(context.Context) (npmapi.Meta, error) {
	return func(ctx context.Context) (npmapi.Meta, error) {
		h, err := c.GetProxyHost(ctx, id)
		if err != nil {
			return nil, err
		}
		return h.Meta, nil
	}
}

func newHostRemoveCommand(f *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "rm <id|domain>",
		Aliases: []string{"delete"},
		Short:   "Delete a proxy host",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(f)
			if err != nil {
				return err
			}
			c, err := rt.authenticated()
			if err != nil {
				return err
			}
			target, err := resolveHost(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			op := Op{
				Verb:     "delete",
				Kind:     "proxy-host",
				Resource: fmt.Sprintf("proxy-host %d (%s)", target.ID, target.Name()),
				TargetID: target.ID,
				Method:   "DELETE",
				Path:     fmt.Sprintf("/nginx/proxy-hosts/%d", target.ID),
				Tier:     TierDestructive,
				Fetch:    fetchProxyHost(c, target.ID),
			}
			if err := rt.gate().run(cmd.Context(), op, func(ctx context.Context) error {
				return c.DeleteProxyHost(ctx, target.ID)
			}); err != nil {
				return err
			}
			if f.dryRun {
				return nil
			}
			return output.Render(rt.stdout, rt.format, map[string]any{
				"status": "deleted", "id": target.ID, "domain_names": target.DomainNames,
			})
		},
	}
}

func newHostToggleCommand(f *globalFlags, enable bool) *cobra.Command {
	verb := "disable"
	if enable {
		verb = "enable"
	}
	return &cobra.Command{
		Use:   verb + " <id|domain>",
		Short: verb + " a proxy host",
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
			target, err := resolveHost(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			op := Op{
				Verb:     verb,
				Kind:     "proxy-host",
				Resource: fmt.Sprintf("proxy-host %d (%s)", target.ID, target.Name()),
				TargetID: target.ID,
				Method:   "POST",
				Path:     fmt.Sprintf("/nginx/proxy-hosts/%d/%s", target.ID, verb),
				Tier:     TierNormal,
				Fetch:    fetchProxyHost(c, target.ID),
				Verify:   verifyProxyHost(c, target.ID),
			}
			if err := rt.gate().run(cmd.Context(), op, func(ctx context.Context) error {
				if enable {
					return c.EnableProxyHost(ctx, target.ID)
				}
				return c.DisableProxyHost(ctx, target.ID)
			}); err != nil {
				return err
			}
			if f.dryRun {
				return nil
			}
			return output.Render(rt.stdout, rt.format, map[string]any{"status": verb + "d", "id": target.ID})
		},
	}
}
