package npmapi

import "context"

// VersionCheck is the response of GET /version/check.
type VersionCheck struct {
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"update_available"`
}

// CheckVersion reports the running and latest available NPM versions.
func (c *Client) CheckVersion(ctx context.Context) (*VersionCheck, error) {
	var v VersionCheck
	if err := c.do(ctx, request{method: "GET", path: "/version/check"}, &v); err != nil {
		return nil, err
	}
	return &v, nil
}
