package auth

import (
	"os"
	"strings"
)

// Environment variable names.
//
// Every variable is prefixed NPMCTL_. In particular NPM_TOKEN is deliberately
// NOT read: it is the npm registry's variable, and honouring it would let an
// unrelated CI secret authenticate against a proxy manager.
const (
	EnvToken      = "NPMCTL_TOKEN"
	EnvURL        = "NPMCTL_URL"
	EnvIdentity   = "NPMCTL_IDENTITY"
	EnvPassword   = "NPMCTL_PASSWORD"
	EnvProfile    = "NPMCTL_PROFILE"
	EnvAllowWrite = "NPMCTL_ALLOW_WRITE"
	EnvE2EURL     = "NPMCTL_E2E_URL"
)

// EnvCredential builds a credential from the environment, or nil when no token
// is present. Used for CI and containers, where a keyring does not exist.
func EnvCredential(profile, url, identity string) *Credential {
	tok := strings.TrimSpace(os.Getenv(EnvToken))
	if tok == "" {
		return nil
	}
	return &Credential{Profile: profile, URL: url, Identity: identity, Token: tok}
}

// WriteAllowed reports whether the out-of-argv write factor is set.
//
// This lives in an environment variable rather than a flag by design: an agent
// composing a command line cannot supply it without a separate, visible action,
// so the host tool's permission prompt remains the human checkpoint.
func WriteAllowed() bool {
	return strings.TrimSpace(os.Getenv(EnvAllowWrite)) == "1"
}
