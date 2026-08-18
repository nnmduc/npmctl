// `npmctl schema get` and `npmctl schema check` — the drift detector.
package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/nnmduc/npmctl/internal/exitcode"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/nnmduc/npmctl/internal/schema"
	"github.com/nnmduc/npmctl/internal/schemadata"
	"github.com/spf13/cobra"
)

func newSchemaCommand(f *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Fetch and compare the NPM API schema",
		Long: "npmctl is written against the Nginx Proxy Manager 2.15.1 OpenAPI document, vendored\n" +
			"as a test fixture rather than used to generate code.\n\n" +
			"`schema check` compares a live instance against that copy so an upgrade that changes\n" +
			"a request shape is caught deliberately, instead of surfacing as a puzzling 400.\n\n" +
			"It cannot detect BEHAVIOURAL drift — partial-update semantics, the TLS flag coercion\n" +
			"cascade, revoke-on-delete — because no schema expresses those. The lab instance and\n" +
			"the opt-in E2E tests cover that gap.",
	}
	cmd.AddCommand(newSchemaGetCommand(f), newSchemaCheckCommand(f))
	return cmd
}

func newSchemaGetCommand(f *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Fetch the live instance's OpenAPI document",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := newRuntime(f)
			if err != nil {
				return err
			}
			c, err := rt.anonClient()
			if err != nil {
				return err
			}
			doc, err := c.Schema(cmd.Context())
			if err != nil {
				return err
			}
			// Always JSON: this is a document, not a report, and a table would mangle it.
			enc := json.NewEncoder(rt.stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(doc)
		},
	}
}

func newSchemaCheckCommand(f *globalFlags) *cobra.Command {
	var fromFile, vendoredPath string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Compare a schema against the vendored 2.15.1 copy",
		Long: "Exits 0 when the schemas are equivalent, 1 when an implemented path has drifted.\n\n" +
			"Drift confined to paths v1 does not implement (the /users family, PUT /settings/{id})\n" +
			"is reported as informational and does not fail: it cannot break this binary.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := newRuntime(f)
			if err != nil {
				return err
			}
			vendored, err := loadVendoredSchema(vendoredPath)
			if err != nil {
				return err
			}

			var live map[string]any
			if fromFile != "" {
				live, err = readSchemaFile(fromFile)
			} else {
				live, err = fetchLiveSchema(cmd, rt)
			}
			if err != nil {
				return err
			}

			report := schema.Compare(vendored, live)
			if err := output.Render(rt.stdout, rt.format, report); err != nil {
				return err
			}
			if report.Breaking() {
				return exitcode.New(exitcode.Generic,
					"schema drift affects paths npmctl implements: review the findings above")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "compare against a schema file instead of a live instance")
	cmd.Flags().StringVar(&vendoredPath, "vendored", "", "path to the vendored schema (default: the embedded copy)")
	return cmd
}

func fetchLiveSchema(cmd *cobra.Command, rt *runtime) (map[string]any, error) {
	c, err := rt.anonClient()
	if err != nil {
		return nil, err
	}
	return c.Schema(cmd.Context())
}

func readSchemaFile(path string) (map[string]any, error) {
	b, err := os.ReadFile(expandUser(path))
	if err != nil {
		return nil, exitcode.Wrap(exitcode.Usage, err, "read schema file")
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, exitcode.Wrap(exitcode.Usage, err, "parse %s", path)
	}
	return doc, nil
}

// loadVendoredSchema reads the reference copy, preferring an explicit path so the
// command works in a checkout as well as from an installed binary.
func loadVendoredSchema(path string) (map[string]any, error) {
	if path != "" {
		return readSchemaFile(path)
	}
	doc, err := schemadata.Document()
	if err != nil {
		return nil, fmt.Errorf("load vendored schema: %w", err)
	}
	return doc, nil
}
