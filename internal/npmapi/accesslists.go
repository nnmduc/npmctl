package npmapi

import "context"

// AccessListFields is the exact property set POST and PUT accept.
//
// The names are `items` and `clients`. An earlier draft used access_items and
// access_clients — those are the names of the SHARED DEFINITIONS in common.json,
// not the request-body properties, and sending them returns 400.
var AccessListFields = []string{"name", "satisfy_any", "pass_auth", "items", "clients"}

// NewAccessListPayload returns a builder for an access-list body.
func NewAccessListPayload() *Payload { return NewPayload("access-list", AccessListFields) }

// AccessListItem is one basic-auth user.
//
// Password is ALWAYS empty on read: NPM never returns it. That single fact makes
// naive read-modify-write dangerous, because writing the object back sends an empty
// password for every existing user, and the schema sets no minLength.
type AccessListItem struct {
	ID       int    `json:"id,omitempty"`
	Username string `json:"username"`
	Password string `json:"password"`
	Hint     string `json:"hint,omitempty"`
}

// AccessListClient is one allow/deny rule.
type AccessListClient struct {
	ID        int    `json:"id,omitempty"`
	Address   string `json:"address"`
	Directive string `json:"directive"`
}

// AccessList is a basic-auth and/or IP access rule set.
type AccessList struct {
	Timestamps
	ID             int                `json:"id"`
	OwnerUserID    int                `json:"owner_user_id,omitempty"`
	Name           string             `json:"name"`
	SatisfyAny     bool               `json:"satisfy_any"`
	PassAuth       bool               `json:"pass_auth"`
	ProxyHostCount int                `json:"proxy_host_count"`
	Items          []AccessListItem   `json:"items,omitempty"`
	Clients        []AccessListClient `json:"clients,omitempty"`
	Meta           Meta               `json:"meta,omitempty"`
	Owner          *Owner             `json:"owner,omitempty"`
}

func (a *AccessList) GetID() int            { return a.ID }
func (a *AccessList) GetModifiedOn() string { return a.ModifiedOn }

func (c *Client) accessLists() resource[AccessList] {
	return resource[AccessList]{c: c, path: "/nginx/access-lists"}
}

// ListAccessLists returns every access list.
func (c *Client) ListAccessLists(ctx context.Context, expand ...string) ([]AccessList, error) {
	return c.accessLists().list(ctx, expand...)
}

// GetAccessList returns one access list.
func (c *Client) GetAccessList(ctx context.Context, id int, expand ...string) (*AccessList, error) {
	return c.accessLists().get(ctx, id, expand...)
}

// CreateAccessList creates an access list.
func (c *Client) CreateAccessList(ctx context.Context, body map[string]any) (*AccessList, error) {
	return c.accessLists().create(ctx, body)
}

// UpdateAccessList replaces an access list's contents.
//
// items and clients are REPLACED wholesale, not merged. Callers must supply the
// complete arrays.
func (c *Client) UpdateAccessList(ctx context.Context, id int, body map[string]any) (*AccessList, error) {
	return c.accessLists().update(ctx, id, body)
}

// DeleteAccessList removes an access list.
func (c *Client) DeleteAccessList(ctx context.Context, id int) error {
	return c.accessLists().remove(ctx, id)
}

// FindAccessListByName resolves a name to an access list.
func (c *Client) FindAccessListByName(ctx context.Context, name string) (*AccessList, error) {
	items, err := c.ListAccessLists(ctx, "items", "clients")
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].Name == name {
			return &items[i], nil
		}
	}
	return nil, notFoundFor("access-list", name, "/nginx/access-lists")
}
