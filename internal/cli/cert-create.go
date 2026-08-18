// Certificate issuance: the attempt journal that enforces the ACME rate-limit
// cooldown, the timeout appropriate to the challenge type, and the three-state
// wait that never reports "may have succeeded".
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/nnmduc/npmctl/internal/certattempt"
	"github.com/nnmduc/npmctl/internal/exitcode"
	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

type certCreateFlags struct {
	domains            []string
	niceName           string
	provider           string
	dnsChallenge       bool
	dnsProvider        string
	dnsCredentials     string
	propagationSeconds int
	keyType            string
	wait               bool
	timeout            time.Duration
}

func newCertCreateCommand(f *globalFlags) *cobra.Command {
	cf := &certCreateFlags{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Request a certificate",
		Long: "Requests a certificate from NPM. For provider=letsencrypt this runs a real ACME\n" +
			"order, which counts against Let's Encrypt's limit of 5 duplicate certificates per\n" +
			"week.\n\n" +
			"npmctl records every attempt per domain set and refuses a 4th inside 7 days\n" +
			"(--force overrides). It never retries automatically: a failed order is reported,\n" +
			"not repeated.\n\n" +
			"--wait (default) polls until the outcome is certain and reports exactly one of\n" +
			"ISSUED, NOT PRESENT, or INDETERMINATE.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			if err := requireFlags(map[string]bool{"--domain": len(cf.domains) > 0}); err != nil {
				return err
			}
			body, err := cf.payload(cmd)
			if err != nil {
				return err
			}

			// R6: refuse before spending the quota, not after.
			attempts, err := certattempt.New()
			if err != nil {
				return err
			}
			key := certattempt.Key(rt.profileName, cf.domains)
			if cf.provider == "letsencrypt" && !f.dryRun {
				if err := attempts.Check(key, time.Now(), f.force); err != nil {
					return err
				}
			}

			deadline := cf.deadline()
			slow := c.WithTimeout(time.Until(deadline) + 30*time.Second)

			var created *npmapi.Certificate
			op := Op{
				Verb:     "create",
				Kind:     "certificate",
				Resource: fmt.Sprintf("certificate for %v", cf.domains),
				Method:   "POST",
				Path:     "/nginx/certificates",
				Body:     body,
				Tier:     TierNormal,
				Note:     cf.note(),
			}
			runErr := rt.gate().run(cmd.Context(), op, func(ctx context.Context) error {
				// Record the attempt BEFORE the request. A request that times out
				// still consumed an order server-side, so recording afterwards would
				// undercount exactly the case the journal exists for.
				if cf.provider == "letsencrypt" {
					_ = attempts.Record(key, fmt.Sprint(cf.domains), "requested", time.Now())
				}
				created, err = slow.CreateCertificate(ctx, body)
				return err
			})

			// A transport failure mid-order is the ambiguous case: poll rather than
			// guess, and never re-issue.
			if runErr != nil {
				if cf.wait && !f.dryRun && exitcode.Of(runErr) == exitcode.Network {
					fmt.Fprintf(rt.stderr, "the request did not complete cleanly; checking whether the order landed\n")
					return cf.reportPoll(cmd.Context(), rt, c, attempts, key, deadline)
				}
				return runErr
			}
			if f.dryRun {
				return nil
			}
			if cf.wait && cf.provider == "letsencrypt" {
				return cf.reportPoll(cmd.Context(), rt, c, attempts, key, deadline)
			}
			return output.Render(rt.stdout, rt.format, created)
		},
	}
	cmd.Flags().StringSliceVar(&cf.domains, "domain", nil, "domain to certify (repeatable)")
	cmd.Flags().StringVar(&cf.niceName, "name", "", "display name (defaults to the first domain)")
	cmd.Flags().StringVar(&cf.provider, "provider", "letsencrypt", "certificate provider: letsencrypt or other")
	cmd.Flags().BoolVar(&cf.dnsChallenge, "dns-challenge", false, "use the DNS-01 challenge instead of HTTP-01")
	cmd.Flags().StringVar(&cf.dnsProvider, "dns-provider", "", "DNS-01 provider name (see `npmctl cert dns-providers`)")
	cmd.Flags().StringVar(&cf.dnsCredentials, "dns-credentials", "", "DNS-01 provider credentials (never echoed)")
	cmd.Flags().IntVar(&cf.propagationSeconds, "propagation-seconds", 0, "DNS propagation wait before validation")
	cmd.Flags().StringVar(&cf.keyType, "key-type", "", "key type: rsa or ecdsa")
	cmd.Flags().BoolVar(&cf.wait, "wait", true, "poll until the outcome is certain")
	cmd.Flags().DurationVar(&cf.timeout, "timeout", 0, "override the issuance deadline")
	return cmd
}

// payload builds the create body. Only keys the certificate schema permits are
// emitted: `meta` is additionalProperties:false, and letsencrypt_email /
// letsencrypt_agree are NOT valid members of it.
