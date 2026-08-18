package cli

import (
	"context"
	"strconv"

	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

func newCertCommand(f *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "cert",
		Aliases: []string{"certificate"},
		Short:   "Manage SSL certificates",
		Long: "Manage the certificates NPM holds, including Let's Encrypt orders.\n\n" +
			"WARNING: `cert rm` on a Let's Encrypt certificate REVOKES it with the certificate\n" +
			"authority. That is irreversible — the undo journal can restore the database row,\n" +
			"but not the certificate material.\n\n" +
			"Let's Encrypt allows 5 duplicate certificates per week. npmctl tracks issuance\n" +
			"attempts per domain set and refuses a 4th attempt inside 7 days; it never retries\n" +
			"an order automatically.",
	}
	cmd.AddCommand(
		newCertListCommand(f), newCertGetCommand(f), newCertCreateCommand(f),
		newCertRemoveCommand(f), newCertRenewCommand(f), newCertDownloadCommand(f),
		newCertUploadCommand(f), newCertValidateCommand(f),
		newCertTestHTTPCommand(f), newCertDNSProvidersCommand(f),
	)
	return cmd
}

var certColumns = []output.Column{
	{Header: "ID", Key: "id"},
	{Header: "NAME", Key: "nice_name"},
	{Header: "DOMAINS", Key: "domain_names"},
	{Header: "PROVIDER", Key: "provider"},
	{Header: "EXPIRES", Key: "expires_on"},
}

// resolveCert accepts a numeric ID or a domain name.

// resolveCert accepts a numeric ID or a domain name.
func resolveCert(ctx context.Context, c *npmapi.Client, ref string) (*npmapi.Certificate, error) {
	if id, err := strconv.Atoi(ref); err == nil {
		return c.GetCertificate(ctx, id)
	}
	return c.FindCertificateByDomain(ctx, ref)
}

// certificateDependents finds every object that references a certificate.
//
// All four host types can carry certificate_id, and each one loses its TLS material
// on the next nginx reload after the certificate goes away. A delete preview that
// cannot name them is not a preview.

func newCertListCommand(f *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List certificates",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			certs, err := c.ListCertificates(cmd.Context())
			if err != nil {
				return err
			}
			return output.RenderWith(rt.stdout, rt.format, certColumns, certs)
		},
	}
}

func newCertGetCommand(f *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id|domain>",
		Short: "Show one certificate",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			cert, err := resolveCert(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			return output.Render(rt.stdout, rt.format, cert)
		},
	}
}

func newCertDNSProvidersCommand(f *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "dns-providers",
		Short: "List supported DNS-01 providers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			providers, err := c.DNSProviders(cmd.Context())
			if err != nil {
				return err
			}
			return output.Render(rt.stdout, rt.format, providers)
		},
	}
}

func newCertTestHTTPCommand(f *globalFlags) *cobra.Command {
	var domains []string
	cmd := &cobra.Command{
		Use:   "test-http",
		Short: "Test whether the HTTP-01 challenge would reach these domains",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			if err := requireFlags(map[string]bool{"--domain": len(domains) > 0}); err != nil {
				return err
			}
			result, err := c.TestHTTPReach(cmd.Context(), domains)
			if err != nil {
				return err
			}
			return output.Render(rt.stdout, rt.format, result)
		},
	}
	cmd.Flags().StringSliceVar(&domains, "domain", nil, "domain to test (repeatable)")
	return cmd
}
