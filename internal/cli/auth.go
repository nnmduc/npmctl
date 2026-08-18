package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/nnmduc/npmctl/internal/auth"
	"github.com/nnmduc/npmctl/internal/exitcode"
	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

func newAuthCommand(f *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate against an NPM instance",
		Long: "Login mints a bounded 30-day token and stores ONLY that token; the password is\n" +
			"discarded. Tokens are scoped per (profile, url, identity), so a credential for\n" +
			"one instance is never sent to another.",
	}
	cmd.AddCommand(newAuthLoginCommand(f), newAuthLogoutCommand(f), newAuthStatusCommand(f), newAuthWhoamiCommand(f))
	return cmd
}

func newAuthLoginCommand(f *globalFlags) *cobra.Command {
	var identity, expiry string
	var storePassword, allowInsecureAuth bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in and store a bounded token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := newRuntime(f)
			if err != nil {
				return err
			}
			if rt.profile.URL == "" {
				return exitcode.New(exitcode.Usage,
					"no url for profile %q: pass --url https://npm.example.com", rt.profileName)
			}
			if identity == "" {
				identity = rt.profile.Identity
			}
			if identity == "" {
				identity, err = promptLine(rt, "NPM email: ")
				if err != nil {
					return err
				}
			}
			secret := strings.TrimSpace(os.Getenv(auth.EnvPassword))
			if secret == "" {
				secret, err = promptSecret(rt, "NPM password: ")
				if err != nil {
					return err
				}
			}
			if secret == "" {
				return exitcode.New(exitcode.Usage, "no password supplied")
			}

			// Never transmit a password over a connection we cannot trust.
			//
			// Two distinct cases, both refused by default:
			//
			//   - TLS verification switched off: we would be talking to whoever
			//     answered, which is exactly when an interceptor collects the password.
			//   - No TLS at all (http://): strictly worse — the password crosses the
			//     network in cleartext, and so does the bearer token on every later
			//     call. A LAN is not a private channel; anything on the path can read it.
			//
			// --allow-plaintext exists because a trusted lab or an air-gapped LAN is a
			// legitimate judgement call, but it must be made explicitly rather than by
			// default.
			if rt.insecureSkipVerify() && !allowInsecureAuth {
				return exitcode.New(exitcode.Refused,
					"refusing to send a password with TLS verification disabled — "+
						"use --ca-cert or --pin-sha256 instead of --insecure, "+
						"or pass --allow-plaintext to accept the risk")
			}
			if isPlaintextURL(rt.profile.URL) && !allowInsecureAuth {
				return exitcode.New(exitcode.Refused,
					"refusing to send a password over plain HTTP to %s: it, and every "+
						"bearer token afterwards, would cross the network in cleartext. "+
						"Use https://, or pass --allow-plaintext if you accept that on this network.",
					rt.profile.URL)
			}
			if allowInsecureAuth {
				fmt.Fprintln(rt.stderr,
					"warning: --allow-plaintext given; your password and token are sent without transport protection")
			}

			c, err := rt.anonClient()
			if err != nil {
				return err
			}
			tok, err := loginWithOptional2FA(cmd.Context(), rt, c, identity, secret)
			if err != nil {
				return err
			}

			// Mint a bounded token with the freshly obtained one. POST /tokens has
			// no expiry parameter and defaults to 1d; GET /tokens?expiry= does, and
			// is what makes a 30-day credential possible.
			authed, err := npmapi.New(rt.clientOptions(tok.Token))
			if err != nil {
				return err
			}
			minted, err := authed.MintToken(cmd.Context(), expiry)
			if err != nil {
				fmt.Fprintf(rt.stderr,
					"warning: could not mint a %s token (%v); storing the short-lived login token instead\n", expiry, err)
				minted = tok
			}

			cred := &auth.Credential{
				Profile:  rt.profileName,
				URL:      strings.TrimRight(rt.profile.URL, "/"),
				Identity: identity,
				Token:    minted.Token,
				Expires:  minted.Expires,
			}
			if storePassword {
				// Explicit opt-in only. It buys nothing on a 2FA account, where
				// re-login answers with a challenge rather than a token.
				cred.Password = secret
				fmt.Fprintln(rt.stderr,
					"warning: --store-password saved your password; npmctl does not need it to refresh a token")
			}

			res, err := rt.resolver()
			if err != nil {
				return err
			}
			store := res.PreferredStore()
			if err := store.Save(cred); err != nil {
				return err
			}

			// Persist the profile so later calls need no flags.
			prof := rt.cfg.Get(rt.profileName)
			prof.URL = cred.URL
			prof.Identity = identity
			if f.caCert != "" {
				prof.CACert = f.caCert
			}
			if f.pinSHA256 != "" {
				prof.PinSHA256 = f.pinSHA256
			}
			rt.cfg.Upsert(rt.profileName, prof)
			if err := rt.cfg.Save(); err != nil {
				fmt.Fprintf(rt.stderr, "warning: could not save %s: %v\n", rt.cfg.Path(), err)
			}

			return output.Render(rt.stdout, rt.format, map[string]any{
				"status":             "logged in",
				"profile":            rt.profileName,
				"url":                cred.URL,
				"identity":           identity,
				"expires":            cred.Expires,
				"credential_backend": store.Backend(),
				"password_stored":    storePassword,
			})
		},
	}
	cmd.Flags().StringVar(&identity, "identity", "", "NPM account email")
	cmd.Flags().StringVar(&expiry, "expiry", auth.DefaultExpiry, "requested token lifetime, e.g. 30d, 12h, 1y")
	cmd.Flags().BoolVar(&storePassword, "store-password", false,
		"also store the password (opt-in; not needed for token refresh, useless on a 2FA account)")
	cmd.Flags().BoolVar(&allowInsecureAuth, "allow-plaintext", false,
		"permit sending credentials over plain HTTP or with TLS verification disabled")
	return cmd
}

// loginWithOptional2FA performs POST /tokens and completes a challenge when the
// account has two-factor enabled.

// loginWithOptional2FA performs POST /tokens and completes a challenge when the
// account has two-factor enabled.

// loginWithOptional2FA performs POST /tokens and completes a challenge when the
// account has two-factor enabled.
func loginWithOptional2FA(ctx context.Context, rt *runtime, c *npmapi.Client, identity, secret string) (*npmapi.Token, error) {
	tok, challenge, err := c.RequestToken(ctx, identity, secret)
	if err != nil {
		return nil, err
	}
	if tok != nil {
		return tok, nil
	}
	// The challenge TTL is 5 minutes, so prompt immediately rather than doing any
	// other work first.
	fmt.Fprintln(rt.stderr, "two-factor authentication required")
	code, err := promptLine(rt, "TOTP code: ")
	if err != nil {
		return nil, err
	}
	// Kept as a string end to end: TOTP codes legitimately start with zeros, and
	// parsing to an integer would turn 012345 into 12345.
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, exitcode.New(exitcode.Usage, "no 2FA code supplied")
	}
	return c.Verify2FA(ctx, challenge.ChallengeToken, code)
}
