// Client construction and credential resolution for the active profile.
package cli

import (
	"time"

	"github.com/nnmduc/npmctl/internal/auth"
	"github.com/nnmduc/npmctl/internal/npmapi"
)

// insecureSkipVerify resolves the tri-state --insecure against the profile.
func (r *runtime) insecureSkipVerify() bool {
	switch r.flags.insecure {
	case "true":
		return true
	case "false":
		return false
	default:
		return r.profile.InsecureSkipVerify
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// clientOptions assembles transport settings from flags and profile.

// clientOptions assembles transport settings from flags and profile.
func (r *runtime) clientOptions(token string) npmapi.Options {
	return npmapi.Options{
		BaseURL:   r.profile.URL,
		Token:     token,
		Insecure:  r.insecureSkipVerify(),
		CACert:    firstNonEmpty(r.flags.caCert, r.profile.CACert),
		PinSHA256: firstNonEmpty(r.flags.pinSHA256, r.profile.PinSHA256),
		Timeout:   r.flags.timeout,
		Verbose:   r.flags.verbose,
		VerboseTo: r.stderr,
	}
}

// anonClient builds a client with no credential, for health and login.

// anonClient builds a client with no credential, for health and login.
func (r *runtime) anonClient() (*npmapi.Client, error) {
	return npmapi.New(r.clientOptions(""))
}

// resolver builds the credential resolution chain for the active profile.

// resolver builds the credential resolution chain for the active profile.
func (r *runtime) resolver() (*auth.Resolver, error) {
	fs, err := auth.NewFileStore()
	if err != nil {
		return nil, err
	}
	return &auth.Resolver{
		Profile:   r.profileName,
		URL:       r.profile.URL,
		Identity:  r.profile.Identity,
		FlagToken: firstNonEmpty(r.flags.token, ""),
		Keyring:   &auth.KeyringStore{},
		File:      fs,
		Warn:      r.stderr,
	}, nil
}

// authenticated returns a client carrying a valid bearer token, refreshing it
// when it is close to expiry. A missing or expired credential surfaces as exit 9
// rather than an automatic re-login.

// authenticated returns a client carrying a valid bearer token, refreshing it
// when it is close to expiry. A missing or expired credential surfaces as exit 9
// rather than an automatic re-login.
func (r *runtime) authenticated() (*npmapi.Client, error) {
	if r.client != nil {
		return r.client, nil
	}
	res, err := r.resolver()
	if err != nil {
		return nil, err
	}
	resolved, err := res.Resolve()
	if err != nil {
		return nil, err
	}
	// Refresh needs an authenticated client, so build one with the current token
	// first and let Ensure decide whether to mint a replacement.
	c, err := npmapi.New(r.clientOptions(resolved.Credential.Token))
	if err != nil {
		return nil, err
	}
	var save auth.Store
	if resolved.Backend != "--token flag" && resolved.Backend != auth.EnvToken+" env" {
		save = res.PreferredStore()
	}
	cred, err := resolved.Ensure(newCtx(), minter{c}, save, time.Now())
	if err != nil {
		return nil, err
	}
	if cred.Token != resolved.Credential.Token {
		if c, err = npmapi.New(r.clientOptions(cred.Token)); err != nil {
			return nil, err
		}
	}
	r.resolved = resolved
	r.client = c
	return c, nil
}
