// Session inspection and teardown: logout, status and whoami.
package cli

import (
	"time"

	"github.com/nnmduc/npmctl/internal/auth"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

func newAuthLogoutCommand(f *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Delete the stored credential for a profile",
		RunE: func(_ *cobra.Command, _ []string) error {
			rt, err := newRuntime(f)
			if err != nil {
				return err
			}
			fs, err := auth.NewFileStore()
			if err != nil {
				return err
			}
			// Remove from both backends and every URL this profile ever used: a
			// URL change would otherwise orphan the old entry forever.
			removed, _ := fs.DeleteProfile(rt.profileName)
			kr := &auth.KeyringStore{}
			if kr.Available() {
				if err := kr.Delete(rt.profileName, rt.profile.URL, rt.profile.Identity); err == nil {
					removed++
				}
			}
			return output.Render(rt.stdout, rt.format, map[string]any{
				"status":  "logged out",
				"profile": rt.profileName,
				"removed": removed,
			})
		},
	}
}

func newAuthStatusCommand(f *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the active profile, credential backend and token expiry",
		RunE: func(_ *cobra.Command, _ []string) error {
			rt, err := newRuntime(f)
			if err != nil {
				return err
			}
			out := map[string]any{
				"profile":                   rt.profileName,
				"url":                       rt.profile.URL,
				"identity":                  rt.profile.Identity,
				"config":                    rt.cfg.Path(),
				"profiles":                  rt.cfg.Names(),
				"tls_verification_disabled": rt.insecureSkipVerify(),
			}
			res, err := rt.resolver()
			if err != nil {
				return err
			}
			resolved, err := res.Resolve()
			if err != nil {
				out["authenticated"] = false
				out["detail"] = err.Error()
				return output.Render(rt.stdout, rt.format, out)
			}
			// Backend is reported unconditionally so the operator never has to
			// guess where the token actually went.
			out["authenticated"] = true
			out["credential_backend"] = resolved.Backend
			out["expires"] = resolved.Credential.Expires
			out["expired"] = resolved.Credential.Expired(time.Now())
			out["needs_refresh"] = resolved.Credential.NeedsRefresh(time.Now())
			out["password_stored"] = resolved.Credential.Password != ""
			return output.Render(rt.stdout, rt.format, out)
		},
	}
}

func newAuthWhoamiCommand(f *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the identity the stored token authenticates as",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := newRuntime(f)
			if err != nil {
				return err
			}
			c, err := rt.authenticated()
			if err != nil {
				return err
			}
			// A successful authenticated call is the proof; GET /tokens both
			// validates the token and reports its refreshed expiry.
			tok, err := c.MintToken(cmd.Context(), auth.DefaultExpiry)
			if err != nil {
				return err
			}
			return output.Render(rt.stdout, rt.format, map[string]any{
				"profile":  rt.profileName,
				"url":      rt.profile.URL,
				"identity": rt.profile.Identity,
				"valid":    true,
				"expires":  tok.Expires,
			})
		},
	}
}
