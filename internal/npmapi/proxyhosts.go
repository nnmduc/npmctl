package npmapi

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// ProxyHostFields is the exact property set both POST and PUT accept for a
// proxy host, taken from the vendored schema. Both bodies are
// additionalProperties:false over the same list; POST additionally requires
// domain_names, forward_scheme, forward_host and forward_port.
var ProxyHostFields = []string{
	"domain_names", "forward_scheme", "forward_host", "forward_port",
	"certificate_id", "ssl_forced", "hsts_enabled", "hsts_subdomains", "http2_support",
	"block_exploits", "caching_enabled", "allow_websocket_upgrade", "trust_forwarded_proto",
	"access_list_id", "advanced_config", "enabled", "locations", "meta",
}

// NewProxyHostPayload returns a builder for a proxy-host write body.
func NewProxyHostPayload() *Payload { return NewPayload("proxy-host", ProxyHostFields) }

// ProxyHost is a reverse-proxy entry.
//
// CertificateID is `any` because the schema types it anyOf[integer, "new"]: the
// literal string "new" tells NPM to order a certificate inline, which turns an
// ordinary host write into a blocking ACME request.
type ProxyHost struct {
	Timestamps
	ID                    int      `json:"id"`
	OwnerUserID           int      `json:"owner_user_id,omitempty"`
	DomainNames           []string `json:"domain_names"`
	ForwardScheme         string   `json:"forward_scheme"`
	ForwardHost           string   `json:"forward_host"`
	ForwardPort           int      `json:"forward_port"`
	CertificateID         any      `json:"certificate_id,omitempty"`
	SSLForced             bool     `json:"ssl_forced"`
	HSTSEnabled           bool     `json:"hsts_enabled"`
	HSTSSubdomains        bool     `json:"hsts_subdomains"`
	HTTP2Support          bool     `json:"http2_support"`
	BlockExploits         bool     `json:"block_exploits"`
	CachingEnabled        bool     `json:"caching_enabled"`
	AllowWebsocketUpgrade bool     `json:"allow_websocket_upgrade"`
	TrustForwardedProto   bool     `json:"trust_forwarded_proto"`
	AccessListID          int      `json:"access_list_id"`
	AdvancedConfig        string   `json:"advanced_config,omitempty"`
	Enabled               bool     `json:"enabled"`
	Locations             []any    `json:"locations,omitempty"`
	Meta                  Meta     `json:"meta,omitempty"`
	Owner                 *Owner   `json:"owner,omitempty"`
}

// Name returns a human label for previews and journal entries.
func (h *ProxyHost) Name() string {
	if len(h.DomainNames) > 0 {
		return h.DomainNames[0]
	}
	return fmt.Sprintf("proxy-host %d", h.ID)
}

const proxyHostsPath = "/nginx/proxy-hosts"

// expandQuery builds the ?expand= parameter.
//
// It MUST be a single comma-separated value, not repeated parameters. Every NPM route
// parses it as `typeof req.query.expand === "string" ? req.query.expand.split(",") : null`,
// so `expand=items&expand=clients` arrives as an array, fails the typeof check, and
// silently becomes null — the expansion is dropped with no error at all. A fixture
// test cannot catch that; only a live instance shows the missing fields.
func expandQuery(expand []string) url.Values {
	q := url.Values{}
	present := make([]string, 0, len(expand))
	for _, e := range expand {
		if e = strings.TrimSpace(e); e != "" {
			present = append(present, e)
		}
	}
	if len(present) > 0 {
		q.Set("expand", strings.Join(present, ","))
	}
	return q
}

// ListProxyHosts returns every proxy host.
func (c *Client) ListProxyHosts(ctx context.Context, expand ...string) ([]ProxyHost, error) {
	var out []ProxyHost
	r := request{method: "GET", path: proxyHostsPath, query: expandQuery(expand)}
	if err := c.do(ctx, r, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetProxyHost returns one proxy host.
func (c *Client) GetProxyHost(ctx context.Context, id int, expand ...string) (*ProxyHost, error) {
	var out ProxyHost
	r := request{method: "GET", path: fmt.Sprintf("%s/%d", proxyHostsPath, id), query: expandQuery(expand)}
	if err := c.do(ctx, r, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateProxyHost creates a proxy host.
func (c *Client) CreateProxyHost(ctx context.Context, body map[string]any) (*ProxyHost, error) {
	var out ProxyHost
	r := request{method: "POST", path: proxyHostsPath, body: body}
	if err := c.do(ctx, r, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateProxyHost applies a partial update.
func (c *Client) UpdateProxyHost(ctx context.Context, id int, body map[string]any) (*ProxyHost, error) {
	var out ProxyHost
	r := request{method: "PUT", path: fmt.Sprintf("%s/%d", proxyHostsPath, id), body: body}
	if err := c.do(ctx, r, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteProxyHost removes a proxy host.
func (c *Client) DeleteProxyHost(ctx context.Context, id int) error {
	return c.do(ctx, request{method: "DELETE", path: fmt.Sprintf("%s/%d", proxyHostsPath, id)}, nil)
}

// EnableProxyHost enables a proxy host.
func (c *Client) EnableProxyHost(ctx context.Context, id int) error {
	return c.do(ctx, request{method: "POST", path: fmt.Sprintf("%s/%d/enable", proxyHostsPath, id)}, nil)
}

// DisableProxyHost disables a proxy host.
func (c *Client) DisableProxyHost(ctx context.Context, id int) error {
	return c.do(ctx, request{method: "POST", path: fmt.Sprintf("%s/%d/disable", proxyHostsPath, id)}, nil)
}

// FindProxyHostByDomain resolves a domain name to a host, so callers never have
// to invent an ID. Matching is exact against every domain on the host.
func (c *Client) FindProxyHostByDomain(ctx context.Context, domain string) (*ProxyHost, error) {
	hosts, err := c.ListProxyHosts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range hosts {
		for _, d := range hosts[i].DomainNames {
			if d == domain {
				return &hosts[i], nil
			}
		}
	}
	return nil, &APIError{Status: 404, Code: 404, Message: fmt.Sprintf("no proxy host serves %q", domain), Method: "GET", Path: proxyHostsPath}
}
