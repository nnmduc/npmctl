package cli

import (
	"context"
	"strconv"

	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

// redirectFlags mirrors the redirection-host write schema.
type redirectFlags struct {
	domains         []string
	forwardScheme   string
	forwardDomain   string
	forwardHTTPCode int
	preservePath    bool
	certificateID   string
	sslForced       bool
	hsts            bool
	hstsSubdomains  bool
	http2           bool
	blockExploits   bool
	advancedConfig  string
}

func (r *redirectFlags) register(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringSliceVar(&r.domains, "domain", nil, "domain to redirect FROM (repeatable)")
	fl.StringVar(&r.forwardScheme, "forward-scheme", "auto", "scheme to redirect to: auto, http or https")
	fl.StringVar(&r.forwardDomain, "forward-domain", "", "domain to redirect TO")
	fl.IntVar(&r.forwardHTTPCode, "http-code", 301, "HTTP status code to answer with")
	fl.BoolVar(&r.preservePath, "preserve-path", true, "keep the request path when redirecting")
	fl.StringVar(&r.certificateID, "certificate-id", "", `certificate ID, or "new" to order one inline`)
	fl.BoolVar(&r.sslForced, "ssl-forced", false, "redirect HTTP to HTTPS")
	fl.BoolVar(&r.hsts, "hsts", false, "enable HSTS")
	fl.BoolVar(&r.hstsSubdomains, "hsts-subdomains", false, "apply HSTS to subdomains")
	fl.BoolVar(&r.http2, "http2", false, "enable HTTP/2")
	fl.BoolVar(&r.blockExploits, "block-exploits", false, "enable common-exploit blocking")
	fl.StringVar(&r.advancedConfig, "advanced-config", "", "raw nginx directives (requires --allow-advanced-config)")
}

func (r *redirectFlags) payload(cmd *cobra.Command) (*npmapi.Payload, error) {
	p := npmapi.NewRedirectionHostPayload()
	fl := cmd.Flags()
	if len(r.domains) > 0 {
		p.Set("domain_names", r.domains)
	}
	p.SetIf(fl.Changed("forward-scheme"), "forward_scheme", r.forwardScheme)
	p.SetIf(fl.Changed("forward-domain"), "forward_domain_name", r.forwardDomain)
	p.SetIf(fl.Changed("http-code"), "forward_http_code", r.forwardHTTPCode)
	p.SetIf(fl.Changed("preserve-path"), "preserve_path", r.preservePath)
	p.SetIf(fl.Changed("ssl-forced"), "ssl_forced", r.sslForced)
	p.SetIf(fl.Changed("hsts"), "hsts_enabled", r.hsts)
	p.SetIf(fl.Changed("hsts-subdomains"), "hsts_subdomains", r.hstsSubdomains)
	p.SetIf(fl.Changed("http2"), "http2_support", r.http2)
	p.SetIf(fl.Changed("block-exploits"), "block_exploits", r.blockExploits)
	p.SetIf(fl.Changed("advanced-config"), "advanced_config", r.advancedConfig)
	if fl.Changed("certificate-id") {
		v, err := certificateValue(r.certificateID)
		if err != nil {
			return nil, err
		}
		p.Set("certificate_id", v)
	}
	return p, nil
}

// createPayload adds the four fields POST requires.
func (r *redirectFlags) createPayload(cmd *cobra.Command) (*npmapi.Payload, error) {
	p, err := r.payload(cmd)
	if err != nil {
		return nil, err
	}
	if err := requireFlags(map[string]bool{
		"--domain":         len(r.domains) > 0,
		"--forward-domain": r.forwardDomain != "",
	}); err != nil {
		return nil, err
	}
	p.Set("forward_scheme", r.forwardScheme)
	p.Set("forward_domain_name", r.forwardDomain)
	p.Set("forward_http_code", r.forwardHTTPCode)
	return p, nil
}

func newRedirectCommand(f *globalFlags) *cobra.Command {
	rf := &redirectFlags{}
	return newCRUDCommand(f, crudSpec[npmapi.RedirectionHost]{
		use:     "redirect",
		aliases: []string{"redirection-host"},
		short:   "Manage redirection hosts",
		long: "Redirection hosts answer a domain with an HTTP redirect to another domain.\n\n" +
			"Accepts a numeric ID or a source domain name. Writes require NPMCTL_ALLOW_WRITE=1 and --yes.",
		kind:    "redirect",
		path:    "/nginx/redirection-hosts",
		refHelp: "<id|domain>",
		columns: []output.Column{
			{Header: "ID", Key: "id"},
			{Header: "FROM", Key: "domain_names"},
			{Header: "TO", Key: "forward_domain_name"},
			{Header: "CODE", Key: "forward_http_code"},
			{Header: "SSL", Key: "ssl_forced"},
			{Header: "ENABLED", Key: "enabled"},
			{Header: "ONLINE", Key: "meta.nginx_online"},
		},
		registerFlags: rf.register,
		createPayload: rf.createPayload,
		updatePayload: rf.payload,
		resolve: func(ctx context.Context, c *npmapi.Client, ref string) (*npmapi.RedirectionHost, error) {
			if id, err := strconv.Atoi(ref); err == nil {
				return c.GetRedirectionHost(ctx, id)
			}
			return c.FindRedirectionHostByDomain(ctx, ref)
		},
		list: func(ctx context.Context, c *npmapi.Client) ([]npmapi.RedirectionHost, error) {
			return c.ListRedirectionHosts(ctx)
		},
		get: func(ctx context.Context, c *npmapi.Client, id int) (*npmapi.RedirectionHost, error) {
			return c.GetRedirectionHost(ctx, id)
		},
		create: func(ctx context.Context, c *npmapi.Client, b map[string]any) (*npmapi.RedirectionHost, error) {
			return c.CreateRedirectionHost(ctx, b)
		},
		update: func(ctx context.Context, c *npmapi.Client, id int, b map[string]any) (*npmapi.RedirectionHost, error) {
			return c.UpdateRedirectionHost(ctx, id, b)
		},
		remove: func(ctx context.Context, c *npmapi.Client, id int) error {
			return c.DeleteRedirectionHost(ctx, id)
		},
		setEnabled: func(ctx context.Context, c *npmapi.Client, id int, on bool) error {
			return c.SetRedirectionHostEnabled(ctx, id, on)
		},
		idOf:             func(h *npmapi.RedirectionHost) int { return h.ID },
		nameOf:           func(h *npmapi.RedirectionHost) string { return firstDomain(h.DomainNames) },
		modifiedOf:       func(h *npmapi.RedirectionHost) string { return h.ModifiedOn },
		metaOf:           func(h *npmapi.RedirectionHost) npmapi.Meta { return h.Meta },
		advancedConfigOf: func(h *npmapi.RedirectionHost) string { return h.AdvancedConfig },
	})
}
