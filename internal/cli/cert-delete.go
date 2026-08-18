// Certificate delete and renew. `cert rm` on a Let's Encrypt certificate revokes it
// with the authority — the one operation in npmctl that no journal can undo.
package cli

import (
	"context"
	"fmt"

	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

// certificateDependents finds every object that references a certificate.
//
// All four host types can carry certificate_id, and each one loses its TLS material
// on the next nginx reload after the certificate goes away. A delete preview that
// cannot name them is not a preview.
func certificateDependents(ctx context.Context, c *npmapi.Client, id int) ([]string, error) {
	var out []string
	matches := func(v any) bool {
		switch n := v.(type) {
		case float64:
			return int(n) == id
		case int:
			return n == id
		}
		return false
	}

	hosts, err := c.ListProxyHosts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range hosts {
		if matches(hosts[i].CertificateID) {
			out = append(out, fmt.Sprintf("proxy-host %d (%s)", hosts[i].ID, hosts[i].Name()))
		}
	}
	redirects, err := c.ListRedirectionHosts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range redirects {
		if matches(redirects[i].CertificateID) {
			out = append(out, fmt.Sprintf("redirect %d (%s)", redirects[i].ID, firstDomain(redirects[i].DomainNames)))
		}
	}
	deadHosts, err := c.ListDeadHosts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range deadHosts {
		if matches(deadHosts[i].CertificateID) {
			out = append(out, fmt.Sprintf("dead-host %d (%s)", deadHosts[i].ID, firstDomain(deadHosts[i].DomainNames)))
		}
	}
	streams, err := c.ListStreams(ctx)
	if err != nil {
		return nil, err
	}
	for i := range streams {
		if matches(streams[i].CertificateID) {
			out = append(out, fmt.Sprintf("stream %d (port %d)", streams[i].ID, streams[i].IncomingPort))
		}
	}
	return out, nil
}

func newCertRemoveCommand(f *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "rm <id|domain>",
		Aliases: []string{"delete"},
		Short:   "Delete a certificate (REVOKES a Let's Encrypt certificate irreversibly)",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			cert, err := resolveCert(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}

			note := ""
			if cert.IsLetsEncrypt() {
				note = "this is a Let's Encrypt certificate: deleting it REVOKES it with the certificate " +
					"authority. Revocation cannot be undone — the undo journal restores the database row, " +
					"not the certificate. Any client pinning it, and any OCSP check, will treat it as revoked."
				fmt.Fprintf(rt.stderr, "WARNING: %s\n", note)
			}

			op := Op{
				Verb:     "delete",
				Kind:     "certificate",
				Resource: fmt.Sprintf("certificate %d (%s)", cert.ID, cert.Name()),
				TargetID: cert.ID,
				Method:   "DELETE",
				Path:     fmt.Sprintf("/nginx/certificates/%d", cert.ID),
				Tier:     TierDestructive,
				Note:     note,
				Fetch: func(ctx context.Context) (any, string, error) {
					cur, err := c.GetCertificate(ctx, cert.ID)
					if err != nil {
						return nil, "", err
					}
					return cur, cur.ModifiedOn, nil
				},
				Dependents: func(ctx context.Context) ([]string, error) {
					return certificateDependents(ctx, c, cert.ID)
				},
			}
			if err := rt.gate().run(cmd.Context(), op, func(ctx context.Context) error {
				return c.DeleteCertificate(ctx, cert.ID)
			}); err != nil {
				return err
			}
			if f.dryRun {
				return nil
			}
			result := map[string]any{"status": "deleted", "id": cert.ID, "name": cert.Name()}
			if cert.IsLetsEncrypt() {
				result["revoked"] = true
				result["warning"] = "the certificate was revoked and cannot be restored"
			}
			return output.Render(rt.stdout, rt.format, result)
		},
	}
}

func newCertRenewCommand(f *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "renew <id|domain>",
		Short: "Renew a certificate",
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
			op := Op{
				Verb:     "renew",
				Kind:     "certificate",
				Resource: fmt.Sprintf("certificate %d (%s)", cert.ID, cert.Name()),
				TargetID: cert.ID,
				Method:   "POST",
				Path:     fmt.Sprintf("/nginx/certificates/%d/renew", cert.ID),
				Tier:     TierNormal,
				Note:     "a renewal contacts the certificate authority and counts against its rate limits",
				Fetch: func(ctx context.Context) (any, string, error) {
					cur, err := c.GetCertificate(ctx, cert.ID)
					if err != nil {
						return nil, "", err
					}
					return cur, cur.ModifiedOn, nil
				},
			}
			// A renewal runs a full ACME order server-side.
			slow := c.WithTimeout(npmapi.CertificateTimeout)
			var renewed *npmapi.Certificate
			if err := rt.gate().run(cmd.Context(), op, func(ctx context.Context) error {
				renewed, err = slow.RenewCertificate(ctx, cert.ID)
				return err
			}); err != nil {
				return err
			}
			if renewed == nil {
				return nil
			}
			return output.Render(rt.stdout, rt.format, renewed)
		},
	}
}
