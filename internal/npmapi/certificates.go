package npmapi

import (
	"context"
	"fmt"
)

// CertificateFields is the POST property set: only `provider` is required.
//
// There is deliberately no update list — NPM exposes NO PUT for certificates.
var CertificateFields = []string{"provider", "nice_name", "domain_names", "meta"}

// CertificateMetaFields is the complete allow-list for certificate `meta`, which
// is additionalProperties:false.
//
// letsencrypt_email and letsencrypt_agree are NOT here, and sending either
// returns "should NOT have additional properties". The Let's Encrypt address comes
// from NPM's own settings, never from the request body.
var CertificateMetaFields = []string{
	"certificate", "certificate_key", "dns_challenge", "dns_provider",
	"dns_provider_credentials", "key_type", "letsencrypt_certificate", "propagation_seconds",
}

// NewCertificatePayload returns a builder for a certificate create body.
func NewCertificatePayload() *Payload { return NewPayload("certificate", CertificateFields) }

// NewCertificateMetaPayload returns a builder for the nested `meta` object.
func NewCertificateMetaPayload() *Payload {
	return NewPayload("certificate meta", CertificateMetaFields)
}

// Certificate is a TLS certificate NPM manages.
type Certificate struct {
	Timestamps
	ID          int      `json:"id"`
	OwnerUserID int      `json:"owner_user_id,omitempty"`
	Provider    string   `json:"provider"`
	NiceName    string   `json:"nice_name"`
	DomainNames []string `json:"domain_names"`
	ExpiresOn   string   `json:"expires_on,omitempty"`
	Meta        Meta     `json:"meta,omitempty"`
	Owner       *Owner   `json:"owner,omitempty"`
}

func (c *Certificate) GetID() int            { return c.ID }
func (c *Certificate) GetDomains() []string  { return c.DomainNames }
func (c *Certificate) GetMeta() Meta         { return c.Meta }
func (c *Certificate) GetModifiedOn() string { return c.ModifiedOn }

// IsLetsEncrypt reports whether deleting this certificate will revoke it.
//
// This is the difference between a reversible and an irreversible delete:
// internal/certificate.js calls revokeLetsEncryptSsl() for any provider
// "letsencrypt", so the certificate material is gone, not merely unreferenced.
func (c *Certificate) IsLetsEncrypt() bool { return c.Provider == "letsencrypt" }

// Name returns a human label.
func (c *Certificate) Name() string {
	if c.NiceName != "" {
		return c.NiceName
	}
	if len(c.DomainNames) > 0 {
		return c.DomainNames[0]
	}
	return fmt.Sprintf("certificate %d", c.ID)
}

const certificatesPath = "/nginx/certificates"

func (c *Client) certificates() resource[Certificate] {
	return resource[Certificate]{c: c, path: certificatesPath}
}

// ListCertificates returns every certificate.
func (c *Client) ListCertificates(ctx context.Context, expand ...string) ([]Certificate, error) {
	return c.certificates().list(ctx, expand...)
}

// GetCertificate returns one certificate.
func (c *Client) GetCertificate(ctx context.Context, id int, expand ...string) (*Certificate, error) {
	return c.certificates().get(ctx, id, expand...)
}

// CreateCertificate requests a certificate. For provider "letsencrypt" this runs a
// full ACME order server-side before responding, so callers should widen the
// timeout to CertificateTimeout.
func (c *Client) CreateCertificate(ctx context.Context, body map[string]any) (*Certificate, error) {
	return c.certificates().create(ctx, body)
}

// DeleteCertificate removes a certificate.
//
// For a letsencrypt certificate this ALSO revokes it with the ACME authority. That
// is irreversible: no undo journal entry can restore revoked material.
func (c *Client) DeleteCertificate(ctx context.Context, id int) error {
	return c.certificates().remove(ctx, id)
}

// RenewCertificate triggers a renewal.
func (c *Client) RenewCertificate(ctx context.Context, id int) (*Certificate, error) {
	var out Certificate
	req := request{method: "POST", path: fmt.Sprintf("%s/%d/renew", certificatesPath, id)}
	if err := c.do(ctx, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DownloadCertificate returns a zip archive of the certificate directory.
//
// The archive contains EVERY .pem in that directory, including privkey.pem, so
// callers must write it to a 0600 file outside the working tree.
func (c *Client) DownloadCertificate(ctx context.Context, id int) ([]byte, error) {
	body, _, err := c.doRaw(ctx, request{
		method: "GET",
		path:   fmt.Sprintf("%s/%d/download", certificatesPath, id),
	})
	return body, err
}

// TestHTTPReach asks NPM whether the HTTP-01 challenge would reach these domains.
func (c *Client) TestHTTPReach(ctx context.Context, domains []string) (map[string]any, error) {
	var out map[string]any
	req := request{
		method: "POST",
		path:   certificatesPath + "/test-http",
		body:   map[string]any{"domains": domains},
	}
	if err := c.do(ctx, req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DNSProviders lists the DNS-01 providers this NPM build supports.
func (c *Client) DNSProviders(ctx context.Context) (any, error) {
	var out any
	if err := c.do(ctx, request{method: "GET", path: certificatesPath + "/dns-providers"}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// FindCertificateByDomain resolves a domain to a certificate.
func (c *Client) FindCertificateByDomain(ctx context.Context, domain string) (*Certificate, error) {
	items, err := c.ListCertificates(ctx)
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
	return nil, notFoundFor("certificate", domain, certificatesPath)
}
