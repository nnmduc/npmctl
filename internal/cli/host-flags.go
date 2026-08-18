package cli

import (
	"strconv"
	"strings"

	"github.com/nnmduc/npmctl/internal/exitcode"
	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

// hostFlags mirrors the proxy-host write schema. Every field is bound to a flag
// so a partial update can send exactly what the caller named and nothing else.
type hostFlags struct {
	domains        []string
	forwardScheme  string
	forwardHost    string
	forwardPort    int
	certificateID  string // string because the schema allows the literal "new"
	sslForced      bool
	hstsEnabled    bool
	hstsSubdomains bool
	http2Support   bool
	blockExploits  bool
	cachingEnabled bool
	websocket      bool
	trustForwarded bool
	accessListID   int
	advancedConfig string
	enabled        bool
}

func (h *hostFlags) register(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringSliceVar(&h.domains, "domain", nil, "domain name to serve (repeatable)")
	fl.StringVar(&h.forwardScheme, "forward-scheme", "http", "upstream scheme: http or https")
	fl.StringVar(&h.forwardHost, "forward-host", "", "upstream host or IP")
	fl.IntVar(&h.forwardPort, "forward-port", 0, "upstream port")
	fl.StringVar(&h.certificateID, "certificate-id", "", `certificate ID, or "new" to order one inline (blocking ACME request)`)
	fl.BoolVar(&h.sslForced, "ssl-forced", false, "redirect HTTP to HTTPS")
	fl.BoolVar(&h.hstsEnabled, "hsts", false, "enable HSTS")
	fl.BoolVar(&h.hstsSubdomains, "hsts-subdomains", false, "apply HSTS to subdomains")
	fl.BoolVar(&h.http2Support, "http2", false, "enable HTTP/2")
	fl.BoolVar(&h.blockExploits, "block-exploits", false, "enable NPM's common-exploit blocking")
	fl.BoolVar(&h.cachingEnabled, "caching", false, "enable asset caching")
	fl.BoolVar(&h.websocket, "websocket-upgrade", false, "allow websocket upgrades")
	fl.BoolVar(&h.trustForwarded, "trust-forwarded-proto", false, "trust an inbound X-Forwarded-Proto header")
	fl.IntVar(&h.accessListID, "access-list-id", 0, "access list to attach (0 detaches)")
	fl.StringVar(&h.advancedConfig, "advanced-config", "", "raw nginx directives (requires --allow-advanced-config)")
	fl.BoolVar(&h.enabled, "enabled", true, "whether the host is enabled")
}

// certificateValue converts the --certificate-id flag to the schema's
// anyOf[integer, "new"]. "new" is preserved verbatim because it is a meaningful
// instruction to NPM, not a placeholder.
func certificateValue(s string) (any, error) {
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "new") {
		return "new", nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil, exitcode.New(exitcode.Usage, `--certificate-id must be an integer or "new", got %q`, s)
	}
	return n, nil
}

// OrdersCertificateInline reports whether a host write will trigger ACME
// issuance, which needs the certificate timeout rather than the default.
func OrdersCertificateInline(v any) bool {
	s, ok := v.(string)
	return ok && strings.EqualFold(s, "new")
}

// payload builds a body containing only the flags the caller actually set.
//
// Using Changed() rather than zero-value checks is what makes a partial update
// correct: --ssl-forced=false and "flag omitted" are different intents, and
// treating them alike would silently re-enable settings the caller never touched.
func (h *hostFlags) payload(cmd *cobra.Command) (*npmapi.Payload, error) {
	p := npmapi.NewProxyHostPayload()
	fl := cmd.Flags()

	if len(h.domains) > 0 {
		p.Set("domain_names", h.domains)
	}
	p.SetIf(fl.Changed("forward-scheme"), "forward_scheme", h.forwardScheme)
	p.SetIf(fl.Changed("forward-host"), "forward_host", h.forwardHost)
	p.SetIf(fl.Changed("forward-port"), "forward_port", h.forwardPort)
	p.SetIf(fl.Changed("ssl-forced"), "ssl_forced", h.sslForced)
	p.SetIf(fl.Changed("hsts"), "hsts_enabled", h.hstsEnabled)
	p.SetIf(fl.Changed("hsts-subdomains"), "hsts_subdomains", h.hstsSubdomains)
	p.SetIf(fl.Changed("http2"), "http2_support", h.http2Support)
	p.SetIf(fl.Changed("block-exploits"), "block_exploits", h.blockExploits)
	p.SetIf(fl.Changed("caching"), "caching_enabled", h.cachingEnabled)
	p.SetIf(fl.Changed("websocket-upgrade"), "allow_websocket_upgrade", h.websocket)
	p.SetIf(fl.Changed("trust-forwarded-proto"), "trust_forwarded_proto", h.trustForwarded)
	p.SetIf(fl.Changed("access-list-id"), "access_list_id", h.accessListID)
	p.SetIf(fl.Changed("advanced-config"), "advanced_config", h.advancedConfig)
	p.SetIf(fl.Changed("enabled"), "enabled", h.enabled)

	if fl.Changed("certificate-id") {
		v, err := certificateValue(h.certificateID)
		if err != nil {
			return nil, err
		}
		p.Set("certificate_id", v)
	}
	return p, nil
}

// createPayload additionally enforces the four fields POST requires, so the
// error names the missing flag instead of surfacing a generic 400.
func (h *hostFlags) createPayload(cmd *cobra.Command) (*npmapi.Payload, error) {
	p, err := h.payload(cmd)
	if err != nil {
		return nil, err
	}
	missing := []string{}
	if len(h.domains) == 0 {
		missing = append(missing, "--domain")
	}
	if h.forwardHost == "" {
		missing = append(missing, "--forward-host")
	}
	if h.forwardPort == 0 {
		missing = append(missing, "--forward-port")
	}
	if len(missing) > 0 {
		return nil, exitcode.New(exitcode.Usage, "missing required flag(s): %s", strings.Join(missing, ", "))
	}
	// POST requires these even when the caller left them at their defaults.
	p.Set("forward_scheme", h.forwardScheme)
	p.Set("forward_host", h.forwardHost)
	p.Set("forward_port", h.forwardPort)
	return p, nil
}

// hostColumns is the table view: the fields an operator scans for, not every
// field the API returns.
var hostColumns = []output.Column{
	{Header: "ID", Key: "id"},
	{Header: "DOMAINS", Key: "domain_names"},
	{Header: "UPSTREAM", Key: "forward_host"},
	{Header: "PORT", Key: "forward_port"},
	{Header: "SSL", Key: "ssl_forced"},
	{Header: "ENABLED", Key: "enabled"},
	{Header: "ONLINE", Key: "meta.nginx_online"},
}
