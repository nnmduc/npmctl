// Certificate create payload construction, challenge-appropriate deadlines, and the
// three-state issuance report.
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

// payload builds the create body. Only keys the certificate schema permits are
// emitted: `meta` is additionalProperties:false, and letsencrypt_email /
// letsencrypt_agree are NOT valid members of it.
func (cf *certCreateFlags) payload(cmd *cobra.Command) (map[string]any, error) {
	p := npmapi.NewCertificatePayload()
	p.Set("provider", cf.provider)
	if len(cf.domains) > 0 {
		p.Set("domain_names", cf.domains)
	}
	name := cf.niceName
	if name == "" && len(cf.domains) > 0 {
		name = cf.domains[0]
	}
	if name != "" {
		p.Set("nice_name", name)
	}

	meta := npmapi.NewCertificateMetaPayload()
	fl := cmd.Flags()
	meta.Set("dns_challenge", cf.dnsChallenge)
	meta.SetIf(fl.Changed("dns-provider"), "dns_provider", cf.dnsProvider)
	meta.SetIf(fl.Changed("dns-credentials"), "dns_provider_credentials", cf.dnsCredentials)
	meta.SetIf(fl.Changed("propagation-seconds"), "propagation_seconds", cf.propagationSeconds)
	meta.SetIf(fl.Changed("key-type"), "key_type", cf.keyType)

	if cf.dnsChallenge && cf.dnsProvider == "" {
		return nil, exitcode.New(exitcode.Usage, "--dns-challenge requires --dns-provider")
	}
	metaMap, err := meta.Map()
	if err != nil {
		return nil, err
	}
	p.Set("meta", metaMap)
	return p.Map()
}

// deadline picks the issuance budget. DNS-01 must wait for propagation before
// validation even starts, so a flat HTTP-01 timeout would abort a healthy order.

// deadline picks the issuance budget. DNS-01 must wait for propagation before
// validation even starts, so a flat HTTP-01 timeout would abort a healthy order.
func (cf *certCreateFlags) deadline() time.Time {
	if cf.timeout > 0 {
		return time.Now().Add(cf.timeout)
	}
	if cf.dnsChallenge {
		return time.Now().Add(npmapi.DNSChallengeTimeout(cf.propagationSeconds))
	}
	return time.Now().Add(npmapi.CertificateTimeout)
}

func (cf *certCreateFlags) note() string {
	if cf.provider != "letsencrypt" {
		return ""
	}
	return "this contacts Let's Encrypt and counts against its limit of 5 duplicate " +
		"certificates per week; npmctl will not retry it automatically"
}

// reportPoll waits for a definite outcome and records it against the attempt.

// reportPoll waits for a definite outcome and records it against the attempt.
func (cf *certCreateFlags) reportPoll(
	ctx context.Context, rt *runtime, c *npmapi.Client,
	attempts *certattempt.Journal, key string, deadline time.Time,
) error {
	// Progress goes to stderr so stdout stays parseable JSON.
	result, err := c.PollIssuance(ctx, cf.domains, deadline, func(msg string) {
		fmt.Fprintf(rt.stderr, "%s\n", msg)
	})
	if err != nil {
		return err
	}
	_ = attempts.Record(key, fmt.Sprint(cf.domains), string(result.State), time.Now())

	if err := output.Render(rt.stdout, rt.format, result); err != nil {
		return err
	}
	switch result.State {
	case npmapi.StateIssued:
		return nil
	case npmapi.StateNotPresent:
		return exitcode.New(exitcode.API, "certificate issuance failed: %s", result.Detail)
	default:
		return exitcode.New(exitcode.Network, "certificate issuance is indeterminate: %s", result.Detail)
	}
}
