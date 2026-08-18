// Read-only administrative commands: audit log, host report, and settings.
//
// Every command in this file is a read. None constructs a gate Op, and a test asserts
// that — the write half of NPM's admin surface (the whole /users family and
// PUT /settings/{id}) is deferred to v2 rather than shipped behind a tier.
package cli

import (
	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

func newAuditLogCommand(f *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "audit-log",
		Aliases: []string{"audit"},
		Short:   "Show the NPM audit log",
		Long: "Reports recent actions recorded by NPM.\n\n" +
			"The server returns at most 100 entries and offers no pagination — there are no\n" +
			"--limit or --offset flags because the API has no such parameters.\n\n" +
			"Audit entries can contain DNS provider credentials: NPM strips them when a\n" +
			"certificate is created but not when one is renewed. npmctl redacts them on the way\n" +
			"out.",
	}
	cmd.AddCommand(newAuditLogListCommand(f), newAuditLogGetCommand(f))
	return cmd
}

var auditLogColumns = []output.Column{
	{Header: "ID", Key: "id"},
	{Header: "WHEN", Key: "created_on"},
	{Header: "ACTION", Key: "action"},
	{Header: "OBJECT", Key: "object_type"},
	{Header: "OBJ-ID", Key: "object_id"},
	{Header: "USER", Key: "user.email"},
}

func newAuditLogListCommand(f *globalFlags) *cobra.Command {
	var expand []string
	var query string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent audit-log entries (server caps this at 100)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			entries, err := c.ListAuditLog(cmd.Context(), npmapi.AuditLogOptions{Expand: expand, Query: query})
			if err != nil {
				return err
			}
			return output.RenderWith(rt.stdout, rt.format, auditLogColumns, entries)
		},
	}
	// Only the two parameters the API actually validates.
	cmd.Flags().StringSliceVar(&expand, "expand", nil, "related objects to include, e.g. user")
	cmd.Flags().StringVar(&query, "query", "", "search string")
	return cmd
}

func newAuditLogGetCommand(f *globalFlags) *cobra.Command {
	var expand []string
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Show one audit-log entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			id, err := parsePositiveInt(args[0], "audit-log id")
			if err != nil {
				return err
			}
			entry, err := c.GetAuditLogEntry(cmd.Context(), id, expand...)
			if err != nil {
				return err
			}
			return output.Render(rt.stdout, rt.format, entry)
		},
	}
	cmd.Flags().StringSliceVar(&expand, "expand", nil, "related objects to include, e.g. user")
	return cmd
}

func newReportCommand(f *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Show NPM reports",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "hosts",
		Short: "Count hosts by type",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			report, err := c.ReportHosts(cmd.Context())
			if err != nil {
				return err
			}
			return output.Render(rt.stdout, rt.format, report)
		},
	})
	return cmd
}

func newSettingsCommand(f *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Read NPM instance settings",
		Long: "Reads the instance's configuration settings.\n\n" +
			"This is read-only in v1. Writing a setting (PUT /settings/{id}) is deferred: setting\n" +
			"IDs are free-form strings whose meaning varies by version, and writing them safely\n" +
			"needs per-setting validation that does not exist yet.",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List all settings",
			RunE: func(cmd *cobra.Command, _ []string) error {
				rt, c, err := authedRuntime(f)
				if err != nil {
					return err
				}
				settings, err := c.ListSettings(cmd.Context())
				if err != nil {
					return err
				}
				return output.RenderWith(rt.stdout, rt.format, []output.Column{
					{Header: "ID", Key: "id"},
					{Header: "NAME", Key: "name"},
					{Header: "VALUE", Key: "value"},
				}, settings)
			},
		},
		&cobra.Command{
			Use:   "get <setting-id>",
			Short: "Show one setting",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				rt, c, err := authedRuntime(f)
				if err != nil {
					return err
				}
				setting, err := c.GetSetting(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return output.Render(rt.stdout, rt.format, setting)
			},
		},
	)
	return cmd
}
