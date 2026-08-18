package cli

import (
	"context"
	"strconv"

	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

// deadHostFlags mirrors the dead-host write schema. A 404 host serves nothing, so
// there is no upstream to configure — only domains and TLS behaviour.
type deadHostFlags struct {
	domains        []string
	certificateID  string
	sslForced      bool
	hsts           bool
	hstsSubdomains bool
	http2          bool
	advancedConfig string
}

func (d *deadHostFlags) register(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringSliceVar(&d.domains, "domain", nil, "domain to answer with 404 (repeatable)")
	fl.StringVar(&d.certificateID, "certificate-id", "", `certificate ID, or "new" to order one inline`)
	fl.BoolVar(&d.sslForced, "ssl-forced", false, "redirect HTTP to HTTPS")
	fl.BoolVar(&d.hsts, "hsts", false, "enable HSTS")
	fl.BoolVar(&d.hstsSubdomains, "hsts-subdomains", false, "apply HSTS to subdomains")
	fl.BoolVar(&d.http2, "http2", false, "enable HTTP/2")
	fl.StringVar(&d.advancedConfig, "advanced-config", "", "raw nginx directives (requires --allow-advanced-config)")
}

func (d *deadHostFlags) payload(cmd *cobra.Command) (*npmapi.Payload, error) {
	p := npmapi.NewDeadHostPayload()
	fl := cmd.Flags()
	if len(d.domains) > 0 {
		p.Set("domain_names", d.domains)
	}
	p.SetIf(fl.Changed("ssl-forced"), "ssl_forced", d.sslForced)
	p.SetIf(fl.Changed("hsts"), "hsts_enabled", d.hsts)
	p.SetIf(fl.Changed("hsts-subdomains"), "hsts_subdomains", d.hstsSubdomains)
	p.SetIf(fl.Changed("http2"), "http2_support", d.http2)
	p.SetIf(fl.Changed("advanced-config"), "advanced_config", d.advancedConfig)
	if fl.Changed("certificate-id") {
		v, err := certificateValue(d.certificateID)
		if err != nil {
			return nil, err
		}
		p.Set("certificate_id", v)
	}
	return p, nil
}

func (d *deadHostFlags) createPayload(cmd *cobra.Command) (*npmapi.Payload, error) {
	p, err := d.payload(cmd)
	if err != nil {
		return nil, err
	}
	// domain_names is the only field POST requires.
	if err := requireFlags(map[string]bool{"--domain": len(d.domains) > 0}); err != nil {
		return nil, err
	}
	return p, nil
}

func newDeadHostCommand(f *globalFlags) *cobra.Command {
	df := &deadHostFlags{}
	return newCRUDCommand(f, crudSpec[npmapi.DeadHost]{
		use: "dead-host",
		// `404` is the name most operators reach for.
		aliases: []string{"404"},
		short:   "Manage 404 hosts",
		long: "404 hosts answer a domain without proxying it anywhere — useful for parking a\n" +
			"domain or absorbing traffic for a hostname you do not serve.\n\n" +
			"Accepts a numeric ID or a domain name. Writes require NPMCTL_ALLOW_WRITE=1 and --yes.",
		kind:    "dead-host",
		path:    "/nginx/dead-hosts",
		refHelp: "<id|domain>",
		columns: []output.Column{
			{Header: "ID", Key: "id"},
			{Header: "DOMAINS", Key: "domain_names"},
			{Header: "SSL", Key: "ssl_forced"},
			{Header: "ENABLED", Key: "enabled"},
			{Header: "ONLINE", Key: "meta.nginx_online"},
		},
		registerFlags: df.register,
		createPayload: df.createPayload,
		updatePayload: df.payload,
		resolve: func(ctx context.Context, c *npmapi.Client, ref string) (*npmapi.DeadHost, error) {
			if id, err := strconv.Atoi(ref); err == nil {
				return c.GetDeadHost(ctx, id)
			}
			return c.FindDeadHostByDomain(ctx, ref)
		},
		list: func(ctx context.Context, c *npmapi.Client) ([]npmapi.DeadHost, error) {
			return c.ListDeadHosts(ctx)
		},
		get: func(ctx context.Context, c *npmapi.Client, id int) (*npmapi.DeadHost, error) {
			return c.GetDeadHost(ctx, id)
		},
		create: func(ctx context.Context, c *npmapi.Client, b map[string]any) (*npmapi.DeadHost, error) {
			return c.CreateDeadHost(ctx, b)
		},
		update: func(ctx context.Context, c *npmapi.Client, id int, b map[string]any) (*npmapi.DeadHost, error) {
			return c.UpdateDeadHost(ctx, id, b)
		},
		remove: func(ctx context.Context, c *npmapi.Client, id int) error {
			return c.DeleteDeadHost(ctx, id)
		},
		setEnabled: func(ctx context.Context, c *npmapi.Client, id int, on bool) error {
			return c.SetDeadHostEnabled(ctx, id, on)
		},
		idOf:             func(h *npmapi.DeadHost) int { return h.ID },
		nameOf:           func(h *npmapi.DeadHost) string { return firstDomain(h.DomainNames) },
		modifiedOf:       func(h *npmapi.DeadHost) string { return h.ModifiedOn },
		metaOf:           func(h *npmapi.DeadHost) npmapi.Meta { return h.Meta },
		advancedConfigOf: func(h *npmapi.DeadHost) string { return h.AdvancedConfig },
	})
}
