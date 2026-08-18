package npmapi

import "context"

// Health is the response of GET / (operationId: health) — the only unauthenticated
// probe NPM offers, and the pre-flight check before a mutation.
type Health struct {
	Status  string `json:"status"`
	Setup   bool   `json:"setup"`
	Version struct {
		Major    int `json:"major"`
		Minor    int `json:"minor"`
		Revision int `json:"revision"`
	} `json:"version"`
}

// Health fetches instance health. This operation was missing from the original
// endpoint map: a path-list parity check passes while omitting it, which is why
// the checklist counts path x method.
func (c *Client) Health(ctx context.Context) (*Health, error) {
	var h Health
	if err := c.do(ctx, request{method: "GET", path: "/", noAuth: true}, &h); err != nil {
		return nil, err
	}
	return &h, nil
}
