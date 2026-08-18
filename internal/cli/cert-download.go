// `cert download` — the archive NPM returns contains every .pem in the certificate
// directory, INCLUDING privkey.pem. Everything here exists to keep that private key
// from landing somewhere it will be read, committed, or logged.
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nnmduc/npmctl/internal/exitcode"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

func newCertDownloadCommand(f *globalFlags) *cobra.Command {
	var outPath string
	var forceStdout bool

	cmd := &cobra.Command{
		Use:   "download <id|domain>",
		Short: "Download a certificate archive (contains the private key)",
		Long: "Downloads the certificate directory as a zip. The archive contains every .pem in\n" +
			"that directory, including privkey.pem.\n\n" +
			"By default it is written mode 0600 into your data directory, never the working\n" +
			"directory and never stdout. Writing into a directory that looks like a git\n" +
			"repository is refused without --force.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			cert, err := resolveCert(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}

			// stdout is refused by default: the archive is binary and carries a private
			// key, so piping it is almost always a mistake — and in an agent context it
			// would land in a transcript.
			if outPath == "-" {
				if !forceStdout {
					return exitcode.New(exitcode.Refused,
						"refusing to write a private key to stdout: pass --force-stdout to override, "+
							"or -o <path> to write a 0600 file")
				}
				if !output.IsTTY(rt.stdout) {
					return exitcode.New(exitcode.Refused,
						"refusing to write a private key to a non-terminal stdout, even with --force-stdout")
				}
			}

			dest, err := resolveCertDest(outPath, cert.ID, cert.Name())
			if err != nil {
				return err
			}
			if outPath != "-" {
				if err := guardCertDest(dest, f.force); err != nil {
					return err
				}
			}

			data, err := c.DownloadCertificate(cmd.Context(), cert.ID)
			if err != nil {
				return err
			}
			if outPath == "-" {
				_, err := rt.stdout.Write(data)
				return err
			}

			if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
				return err
			}
			// O_EXCL: never silently replace existing key material.
			fh, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return exitcode.Wrap(exitcode.Refused, err, "write %s", dest)
			}
			defer fh.Close()
			if _, err := fh.Write(data); err != nil {
				return err
			}
			return output.Render(rt.stdout, rt.format, map[string]any{
				"status":     "downloaded",
				"id":         cert.ID,
				"path":       dest,
				"mode":       "0600",
				"size_bytes": len(data),
				"warning":    "this archive contains the certificate private key",
			})
		},
	}
	cmd.Flags().StringVarP(&outPath, "out", "O", "", "destination file (default: your data directory; - for stdout)")
	cmd.Flags().BoolVar(&forceStdout, "force-stdout", false, "permit writing the archive to stdout on a terminal")
	return cmd
}

// resolveCertDest defaults to the XDG data directory rather than the working
// directory, so a hurried command cannot drop a private key into a source tree.
func resolveCertDest(outPath string, id int, name string) (string, error) {
	if outPath != "" && outPath != "-" {
		return expandUser(outPath), nil
	}
	base := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, name)
	return filepath.Join(base, "npmctl", "certs", fmt.Sprintf("cert-%d-%s.zip", id, safe)), nil
}

// guardCertDest refuses destinations where key material is likely to be committed
// or served.
func guardCertDest(dest string, force bool) error {
	if _, err := os.Stat(dest); err == nil {
		return exitcode.New(exitcode.Refused,
			"refusing to overwrite %s — remove it first, or choose another --out path", dest)
	}
	dir := filepath.Dir(dest)
	if !force {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return exitcode.New(exitcode.Refused,
				"refusing to write a private key into %s: it looks like a git repository. "+
					"Pass --force if you are certain.", dir)
		}
	}
	return nil
}

func expandUser(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
