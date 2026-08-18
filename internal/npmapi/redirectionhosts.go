package npmapi

import "context"

// RedirectionHostFields is the property set POST and PUT accept, from the
// vendored schema. POST additionally requires domain_names, forward_scheme,
// forward_http_code and forward_domain_name.
var RedirectionHostFields = []string{
	"domain_names", "forward_scheme", "forward_http_code", "forward_domain_name",
	"preserve_path", "certificate_id", "ssl_forced", "hsts_enabled", "hsts_subdomains",
	"http2_support", "block_exploits", "advanced_config", "meta",
}

// NewRedirectionHostPayload returns a builder for a redirection-host body.
func NewRedirectionHostPayload() *Payload {
	return NewPayload("redirection-host", RedirectionHostFields)
}

// RedirectionHost is an HTTP redirect entry.
type RedirectionHost struct {
	Timestamps
	ID                int      `json:"id"`
	OwnerUserID       int      `json:"owner_user_id,omitempty"`
	DomainNames       []string `json:"domain_names"`
	ForwardScheme     string   `json:"forward_scheme"`
	ForwardDomainName string   `json:"forward_domain_name"`
	ForwardHTTPCode   int      `json:"forward_http_code"`
	PreservePath      bool     `json:"preserve_path"`
	CertificateID     any      `json:"certificate_id,omitempty"`
	SSLForced         bool     `json:"ssl_forced"`
	HSTSEnabled       bool     `json:"hsts_enabled"`
	HSTSSubdomains    bool     `json:"hsts_subdomains"`
	HTTP2Support      bool     `json:"http2_support"`
	BlockExploits     bool     `json:"block_exploits"`
	AdvancedConfig    string   `json:"advanced_config,omitempty"`
	Enabled           bool     `json:"enabled"`
	Meta              Meta     `json:"meta,omitempty"`
	Owner             *Owner   `json:"owner,omitempty"`
}

func (h *RedirectionHost) GetID() int            { return h.ID }
func (h *RedirectionHost) GetDomains() []string  { return h.DomainNames }
func (h *RedirectionHost) GetMeta() Meta         { return h.Meta }
func (h *RedirectionHost) GetModifiedOn() string { return h.ModifiedOn }

func (c *Client) redirects() resource[RedirectionHost] {
	return resource[RedirectionHost]{c: c, path: "/nginx/redirection-hosts"}
}

// ListRedirectionHosts returns every redirect.
func (c *Client) ListRedirectionHosts(ctx context.Context, expand ...string) ([]RedirectionHost, error) {
	return c.redirects().list(ctx, expand...)
}

// GetRedirectionHost returns one redirect.
func (c *Client) GetRedirectionHost(ctx context.Context, id int, expand ...string) (*RedirectionHost, error) {
	return c.redirects().get(ctx, id, expand...)
}

// CreateRedirectionHost creates a redirect.
func (c *Client) CreateRedirectionHost(ctx context.Context, body map[string]any) (*RedirectionHost, error) {
	return c.redirects().create(ctx, body)
}

// UpdateRedirectionHost applies a partial update.
func (c *Client) UpdateRedirectionHost(ctx context.Context, id int, body map[string]any) (*RedirectionHost, error) {
	return c.redirects().update(ctx, id, body)
}

// DeleteRedirectionHost removes a redirect.
func (c *Client) DeleteRedirectionHost(ctx context.Context, id int) error {
	return c.redirects().remove(ctx, id)
}

// SetRedirectionHostEnabled enables or disables a redirect.
func (c *Client) SetRedirectionHostEnabled(ctx context.Context, id int, enabled bool) error {
	return c.redirects().setEnabled(ctx, id, enabled)
}

// FindRedirectionHostByDomain resolves a domain to a redirect.
func (c *Client) FindRedirectionHostByDomain(ctx context.Context, domain string) (*RedirectionHost, error) {
	r := c.redirects()
	items, err := r.list(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		for _, d := range items[i].DomainNames {
			if d == domain {
				return &items[i], nil
			}
		}
	}
	return nil, notFoundFor("redirect", domain, r.path)
}
