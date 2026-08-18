package npmapi

import (
	"context"
	"net/url"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

// Token is a bearer credential and its expiry.
type Token struct {
	Expires string `json:"expires"`
	Token   string `json:"token"`
}

// Challenge is the two-factor branch of POST /tokens. NPM returns it at HTTP
// 200, not 401 — a status-code-only reading of the response would mistake a
// challenge for a successful login.
type Challenge struct {
	Requires2FA    bool   `json:"requires_2fa"`
	ChallengeToken string `json:"challenge_token"`
}

// tokenResponse decodes the oneOf: either a token or a 2FA challenge.
type tokenResponse struct {
	Expires        string `json:"expires"`
	Token          string `json:"token"`
	Requires2FA    bool   `json:"requires_2fa"`
	ChallengeToken string `json:"challenge_token"`
}

// RequestToken exchanges credentials for a token.
//
// Exactly one of the returns is non-nil on success. A non-nil Challenge means
// the account has 2FA enabled and the caller must complete Verify2FA within the
// challenge's 5-minute TTL.
func (c *Client) RequestToken(ctx context.Context, identity, secret string) (*Token, *Challenge, error) {
	body := map[string]any{
		"identity": identity,
		"secret":   secret,
		"scope":    "user",
	}
	var resp tokenResponse
	r := request{method: "POST", path: "/tokens", body: body, noAuth: true}
	if err := c.do(ctx, r, &resp); err != nil {
		return nil, nil, err
	}
	if resp.Requires2FA || (resp.Token == "" && resp.ChallengeToken != "") {
		return nil, &Challenge{Requires2FA: true, ChallengeToken: resp.ChallengeToken}, nil
	}
	if resp.Token == "" {
		return nil, nil, exitcode.New(exitcode.Auth, "login returned neither a token nor a 2FA challenge")
	}
	return &Token{Expires: resp.Expires, Token: resp.Token}, nil, nil
}

// Verify2FA completes a two-factor challenge.
//
// code is a string throughout, never an integer: the schema types it as a 6-8
// character string and TOTP codes legitimately begin with zeros, which integer
// parsing would silently discard.
func (c *Client) Verify2FA(ctx context.Context, challengeToken, code string) (*Token, error) {
	body := map[string]any{
		"challenge_token": challengeToken,
		"code":            code,
	}
	var tok Token
	r := request{method: "POST", path: "/tokens/2fa", body: body, noAuth: true}
	if err := c.do(ctx, r, &tok); err != nil {
		return nil, err
	}
	if tok.Token == "" {
		return nil, exitcode.New(exitcode.Auth, "2FA verification returned no token")
	}
	return &tok, nil
}

// MintToken refreshes the current bearer into one with an explicit lifetime.
//
// GET /tokens carries no apiValidator: the route passes req.query.expiry
// straight into getFreshToken, where parseDatePeriod accepts
// ^([0-9]+)(y|Q|M|w|d|h|m|s|ms)$. That is what makes a bounded long-lived token
// possible at all — POST /tokens has no expiry parameter and defaults to 1d.
func (c *Client) MintToken(ctx context.Context, expiry string) (*Token, error) {
	q := url.Values{}
	if expiry != "" {
		q.Set("expiry", expiry)
	}
	var tok Token
	r := request{method: "GET", path: "/tokens", query: q}
	if err := c.do(ctx, r, &tok); err != nil {
		return nil, err
	}
	if tok.Token == "" {
		return nil, exitcode.New(exitcode.Auth, "token refresh returned no token")
	}
	return &tok, nil
}
