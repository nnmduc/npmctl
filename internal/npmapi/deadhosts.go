package npmapi

import "context"

// DeadHostFields is the property set POST and PUT accept. POST requires only
// domain_names — a 404 host serves nothing, so it needs no upstream.
var DeadHostFields = []string{
	"domain_names", "certificate_id", "ssl_forced", "hsts_enabled", "hsts_subdomains",
	"http2_support", "advanced_config", "meta",
}

// NewDeadHostPayload returns a builder for a dead-host body.
func NewDeadHostPayload() *Payload { return NewPayload("dead-host", DeadHostFields) }

// DeadHost is a 404 host: a domain NPM answers without proxying anywhere.
type DeadHost struct {
	Timestamps
	ID             int      `json:"id"`
	OwnerUserID    int      `json:"owner_user_id,omitempty"`
	DomainNames    []string `json:"domain_names"`
	CertificateID  any      `json:"certificate_id,omitempty"`
	SSLForced      bool     `json:"ssl_forced"`
	HSTSEnabled    bool     `json:"hsts_enabled"`
	HSTSSubdomains bool     `json:"hsts_subdomains"`
	HTTP2Support   bool     `json:"http2_support"`
	AdvancedConfig string   `json:"advanced_config,omitempty"`
	Enabled        bool     `json:"enabled"`
	Meta           Meta     `json:"meta,omitempty"`
	Owner          *Owner   `json:"owner,omitempty"`
}

func (h *DeadHost) GetID() int            { return h.ID }
func (h *DeadHost) GetDomains() []string  { return h.DomainNames }
func (h *DeadHost) GetMeta() Meta         { return h.Meta }
func (h *DeadHost) GetModifiedOn() string { return h.ModifiedOn }

func (c *Client) deadHosts() resource[DeadHost] {
	return resource[DeadHost]{c: c, path: "/nginx/dead-hosts"}
}

// ListDeadHosts returns every 404 host.
func (c *Client) ListDeadHosts(ctx context.Context, expand ...string) ([]DeadHost, error) {
	return c.deadHosts().list(ctx, expand...)
}

// GetDeadHost returns one 404 host.
func (c *Client) GetDeadHost(ctx context.Context, id int, expand ...string) (*DeadHost, error) {
	return c.deadHosts().get(ctx, id, expand...)
}

// CreateDeadHost creates a 404 host.
func (c *Client) CreateDeadHost(ctx context.Context, body map[string]any) (*DeadHost, error) {
	return c.deadHosts().create(ctx, body)
}

// UpdateDeadHost applies a partial update.
func (c *Client) UpdateDeadHost(ctx context.Context, id int, body map[string]any) (*DeadHost, error) {
	return c.deadHosts().update(ctx, id, body)
}

// DeleteDeadHost removes a 404 host.
func (c *Client) DeleteDeadHost(ctx context.Context, id int) error {
	return c.deadHosts().remove(ctx, id)
}

// SetDeadHostEnabled enables or disables a 404 host.
func (c *Client) SetDeadHostEnabled(ctx context.Context, id int, enabled bool) error {
	return c.deadHosts().setEnabled(ctx, id, enabled)
}

// FindDeadHostByDomain resolves a domain to a 404 host.
func (c *Client) FindDeadHostByDomain(ctx context.Context, domain string) (*DeadHost, error) {
	r := c.deadHosts()
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
	return nil, notFoundFor("dead-host", domain, r.path)
}
