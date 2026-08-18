package cli

import (
	"context"

	"github.com/nnmduc/npmctl/internal/auth"
	"github.com/nnmduc/npmctl/internal/npmapi"
)

// minter adapts *npmapi.Client to auth.Minter, keeping the auth package free of
// a dependency on the API client.
type minter struct{ c *npmapi.Client }

func (m minter) MintToken(ctx context.Context, expiry string) (*auth.Token, error) {
	t, err := m.c.MintToken(ctx, expiry)
	if err != nil {
		return nil, err
	}
	return &auth.Token{Expires: t.Expires, Token: t.Token}, nil
}

func newCtx() context.Context { return context.Background() }
