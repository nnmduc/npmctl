// `host create`, including the widened timeout for a write that orders a
// certificate inline via certificate_id:"new".
package cli

import (
	"context"
	"fmt"

	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

func newHostCreateCommand(f *globalFlags) *cobra.Command {
	hf := &hostFlags{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a proxy host",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := newRuntime(f)
			if err != nil {
				return err
			}
			c, err := rt.authenticated()
			if err != nil {
				return err
			}
			p, err := hf.createPayload(cmd)
			if err != nil {
				return err
			}
			body, err := p.Map()
			if err != nil {
				return err
			}
			client := certTimeoutClient(c, body)

			var created *npmapi.ProxyHost
			op := Op{
				Verb:                  "create",
				Kind:                  "proxy-host",
				Resource:              fmt.Sprintf("proxy-host %v", hf.domains),
				Method:                "POST",
				Path:                  "/nginx/proxy-hosts",
				Body:                  body,
				Tier:                  TierNormal,
				TouchesAdvancedConfig: cmd.Flags().Changed("advanced-config"),
				Verify: func(ctx context.Context) (npmapi.Meta, error) {
					if created == nil {
						return nil, nil
					}
					fresh, err := c.GetProxyHost(ctx, created.ID)
					if err != nil {
						return nil, err
					}
					return fresh.Meta, nil
				},
			}
			if err := rt.gate().run(cmd.Context(), op, func(ctx context.Context) error {
				created, err = client.CreateProxyHost(ctx, body)
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
	hf.register(cmd)
	return cmd
}

// certTimeoutClient widens the timeout when a write orders a certificate inline.
// certificate_id:"new" makes NPM run a full ACME order before answering, which
// comfortably outlives the default 30s.

// certTimeoutClient widens the timeout when a write orders a certificate inline.
// certificate_id:"new" makes NPM run a full ACME order before answering, which
// comfortably outlives the default 30s.

// certTimeoutClient widens the timeout when a write orders a certificate inline.
// certificate_id:"new" makes NPM run a full ACME order before answering, which
// comfortably outlives the default 30s.
func certTimeoutClient(c *npmapi.Client, body map[string]any) *npmapi.Client {
	if v, ok := body["certificate_id"]; ok && OrdersCertificateInline(v) {
		return c.WithTimeout(npmapi.CertificateTimeout)
	}
	return c
}
