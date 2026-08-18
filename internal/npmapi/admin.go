// Read-only administrative endpoints: audit log, host report, settings, and the
// live schema. Nothing here writes.
package npmapi

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// AuditLogEntry is one recorded action.
//
// Meta carries the changed object, and for a DNS-01 certificate RENEWAL it contains
// the provider credentials in plaintext: internal/certificate.js applies omissions()
// on create (:225) but passes `meta: updatedCertificate` raw on renew (:916). Callers
// must render this through the output scrubber, never directly.
type AuditLogEntry struct {
	Timestamps
	ID         int    `json:"id"`
	UserID     int    `json:"user_id"`
	ObjectType string `json:"object_type"`
	ObjectID   int    `json:"object_id"`
	Action     string `json:"action"`
	Meta       Meta   `json:"meta,omitempty"`
	User       *Owner `json:"user,omitempty"`
}

const auditLogPath = "/audit-log"

// AuditLogOptions are the ONLY supported query parameters.
//
// There is deliberately no limit or offset: internal/audit-log.js hardcodes
// .limit(100), and the validator whitelist is {expand, query} only. Offering
// pagination flags would imply a capability the API does not have.
type AuditLogOptions struct {
	Expand []string
	Query  string
}

func (o AuditLogOptions) values() url.Values {
	q := expandQuery(o.Expand)
	if s := strings.TrimSpace(o.Query); s != "" {
		q.Set("query", s)
	}
	return q
}

// ListAuditLog returns up to the 100 most recent entries the server will emit.
func (c *Client) ListAuditLog(ctx context.Context, o AuditLogOptions) ([]AuditLogEntry, error) {
	var out []AuditLogEntry
	if err := c.do(ctx, request{method: "GET", path: auditLogPath, query: o.values()}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAuditLogEntry returns one audit-log entry.
func (c *Client) GetAuditLogEntry(ctx context.Context, id int, expand ...string) (*AuditLogEntry, error) {
	var out AuditLogEntry
	req := request{method: "GET", path: fmt.Sprintf("%s/%d", auditLogPath, id), query: expandQuery(expand)}
	if err := c.do(ctx, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// HostReport is the counts NPM reports for each host type.
type HostReport struct {
	Proxy       int `json:"proxy"`
	Redirection int `json:"redirection"`
	Stream      int `json:"stream"`
	Dead        int `json:"dead"`
}

// ReportHosts returns per-type host counts.
func (c *Client) ReportHosts(ctx context.Context) (*HostReport, error) {
	var out HostReport
	if err := c.do(ctx, request{method: "GET", path: "/reports/hosts"}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Setting is one instance setting.
//
// Value is `any` because settings are free-form: the schema types settingID as a
// plain non-empty string and the deployed set is small and version-dependent, so
// modelling per-setting types would invent structure NPM does not guarantee.
type Setting struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Value       any    `json:"value,omitempty"`
	Meta        Meta   `json:"meta,omitempty"`
}

// ListSettings returns every setting.
func (c *Client) ListSettings(ctx context.Context) ([]Setting, error) {
	var out []Setting
	if err := c.do(ctx, request{method: "GET", path: "/settings"}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetSetting returns one setting by its string ID.
func (c *Client) GetSetting(ctx context.Context, id string) (*Setting, error) {
	var out Setting
	if err := c.do(ctx, request{method: "GET", path: "/settings/" + url.PathEscape(id)}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Schema fetches the instance's own OpenAPI document, already dereferenced by the
// server.
func (c *Client) Schema(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	if err := c.do(ctx, request{method: "GET", path: "/schema", noAuth: true}, &out); err != nil {
		return nil, err
	}
	return out, nil
}
