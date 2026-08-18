// `cert upload` and `cert validate` — multipart PEM submission.
//
// The output scrubber matches decoded field names, so a raw multipart body would
// bypass it. These commands therefore never render the body: previews describe
// filenames, sizes and detected material types only.
package cli

import (
	"context"
	"fmt"

	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

type certFileFlags struct {
	certificate  string
	key          string
	intermediate string
}

func (c *certFileFlags) register(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringVar(&c.certificate, "certificate", "", "path to the certificate PEM")
	fl.StringVar(&c.key, "certificate-key", "", "path to the private key PEM")
	fl.StringVar(&c.intermediate, "intermediate-certificate", "", "path to the intermediate chain PEM (optional)")
}

func (c *certFileFlags) load() (*npmapi.CertificateFiles, error) {
	files, err := npmapi.LoadCertificateFiles(c.certificate, c.key, c.intermediate)
	if err != nil {
		return nil, err
	}
	if err := files.Validate(); err != nil {
		return nil, err
	}
	return files, nil
}

func newCertUploadCommand(f *globalFlags) *cobra.Command {
	ff := &certFileFlags{}
	cmd := &cobra.Command{
		Use:   "upload <id|domain>",
		Short: "Upload custom certificate material to an existing certificate",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			cert, err := resolveCert(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			files, err := ff.load()
			if err != nil {
				return err
			}

			op := Op{
				Verb:     "update",
				Kind:     "certificate",
				Resource: fmt.Sprintf("certificate %d (%s)", cert.ID, cert.Name()),
				TargetID: cert.ID,
				Method:   "POST",
				Path:     fmt.Sprintf("/nginx/certificates/%d/upload", cert.ID),
				// Metadata only — never the PEM bytes.
				Body: map[string]any{"files": files.Describe()},
				Tier: TierNormal,
				Fetch: func(ctx context.Context) (any, string, error) {
					cur, err := c.GetCertificate(ctx, cert.ID)
					if err != nil {
						return nil, "", err
					}
					return cur, cur.ModifiedOn, nil
				},
			}
			var result any
			if err := rt.gate().run(cmd.Context(), op, func(ctx context.Context) error {
				result, err = c.UploadCertificate(ctx, cert.ID, files)
				return err
			}); err != nil {
				return err
			}
			if f.dryRun {
				return nil
			}
			return output.Render(rt.stdout, rt.format, result)
		},
	}
	ff.register(cmd)
	return cmd
}

func newCertValidateCommand(f *globalFlags) *cobra.Command {
	ff := &certFileFlags{}
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Ask NPM to parse certificate material without storing it",
		Long: "Validation stores nothing, so it is treated as a read and needs no write gate.\n" +
			"The PEM contents are never printed — only filenames, sizes and detected types.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			files, err := ff.load()
			if err != nil {
				return err
			}
			if f.dryRun {
				return output.Render(rt.stdout, rt.format, map[string]any{
					"dry_run": true,
					"verb":    "validate",
					"method":  "POST",
					"path":    "/nginx/certificates/validate",
					"files":   files.Describe(),
				})
			}
			result, err := validateFiles(cmd.Context(), c, files)
			if err != nil {
				return err
			}
			return output.Render(rt.stdout, rt.format, map[string]any{
				"files":  files.Describe(),
				"result": result,
			})
		},
	}
	ff.register(cmd)
	return cmd
}

func validateFiles(ctx context.Context, c *npmapi.Client, files *npmapi.CertificateFiles) (any, error) {
	return c.ValidateCertificates(ctx, files)
}
