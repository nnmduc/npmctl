package cli

import (
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

func newVersionCommand(f *globalFlags) *cobra.Command {
	var checkRemote bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show the npmctl version, and optionally the NPM instance version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := newRuntime(f)
			if err != nil {
				return err
			}
			out := map[string]any{"npmctl": version}
			if checkRemote {
				c, err := rt.authenticated()
				if err != nil {
					return err
				}
				v, err := c.CheckVersion(cmd.Context())
				if err != nil {
					return err
				}
				out["npm_current"] = v.Current
				out["npm_latest"] = v.Latest
				out["npm_update_available"] = v.UpdateAvailable
			}
			return output.Render(rt.stdout, rt.format, out)
		},
	}
	cmd.Flags().BoolVar(&checkRemote, "check", false, "also query the NPM instance for its version (GET /version/check)")
	return cmd
}

func newHealthCommand(f *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Check NPM instance health (GET /)",
		Long: "Reports the instance's status, setup state and version.\n" +
			"This is the unauthenticated pre-flight probe used before mutations.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := newRuntime(f)
			if err != nil {
				return err
			}
			c, err := rt.anonClient()
			if err != nil {
				return err
			}
			h, err := c.Health(cmd.Context())
			if err != nil {
				return err
			}
			return output.Render(rt.stdout, rt.format, h)
		},
	}
}
