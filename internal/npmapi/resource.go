package npmapi

import (
	"context"
	"fmt"
)

// resource is the shared CRUD shape of NPM's four host-like collections
// (proxy-hosts, redirection-hosts, dead-hosts, streams) plus access-lists.
//
// Extracted only after four concrete implementations existed, so the shape is
// observed rather than guessed. Note what it deliberately does NOT abstract:
// the request bodies. Those differ per resource in ways that matter — PUT
// /streams rejects domain_names while POST accepts it — so each resource keeps
// its own explicit field list.
type resource[T any] struct {
	c    *Client
	path string
}

func (r resource[T]) list(ctx context.Context, expand ...string) ([]T, error) {
	var out []T
	req := request{method: "GET", path: r.path, query: expandQuery(expand)}
	if err := r.c.do(ctx, req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r resource[T]) get(ctx context.Context, id int, expand ...string) (*T, error) {
	var out T
	req := request{method: "GET", path: fmt.Sprintf("%s/%d", r.path, id), query: expandQuery(expand)}
	if err := r.c.do(ctx, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (r resource[T]) create(ctx context.Context, body map[string]any) (*T, error) {
	var out T
	if err := r.c.do(ctx, request{method: "POST", path: r.path, body: body}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (r resource[T]) update(ctx context.Context, id int, body map[string]any) (*T, error) {
	var out T
	req := request{method: "PUT", path: fmt.Sprintf("%s/%d", r.path, id), body: body}
	if err := r.c.do(ctx, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (r resource[T]) remove(ctx context.Context, id int) error {
	return r.c.do(ctx, request{method: "DELETE", path: fmt.Sprintf("%s/%d", r.path, id)}, nil)
}

// setEnabled hits the /enable or /disable sub-resource. Access lists have no such
// endpoint, so only host-like resources call it.
func (r resource[T]) setEnabled(ctx context.Context, id int, enabled bool) error {
	verb := "disable"
	if enabled {
		verb = "enable"
	}
	return r.c.do(ctx, request{method: "POST", path: fmt.Sprintf("%s/%d/%s", r.path, id, verb)}, nil)
}

// hostLike is implemented by every resource that carries domains and nginx meta,
// so the CLI can resolve a domain to an ID and read reload health uniformly.
type hostLike interface {
	GetID() int
	GetDomains() []string
	GetMeta() Meta
	GetModifiedOn() string
}

// findByDomain resolves a domain name to a record, so no caller has to invent an
// ID. Matching is exact against every domain the record serves.
func findByDomain[T hostLike](ctx context.Context, r resource[T], kind, domain string) (*T, error) {
	items, err := r.list(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		for _, d := range items[i].GetDomains() {
			if d == domain {
				return &items[i], nil
			}
		}
	}
	return nil, &APIError{
		Status: 404, Code: 404,
		Message: fmt.Sprintf("no %s serves %q", kind, domain),
		Method:  "GET", Path: r.path,
	}
}
