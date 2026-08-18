package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nnmduc/npmctl/internal/exitcode"
	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/spf13/cobra"
)

// version is overwritten at build time with -ldflags "-X ...cli.version=v1.2.3".
var version = "dev"

// SetVersion lets main inject the build version.
func SetVersion(v string) {
	if v != "" {
		version = v
	}
}

// NewRootCommand builds the command tree. Exported so tests can walk it and
// assert that deferred commands are absent from the binary.
func NewRootCommand() (*cobra.Command, *globalFlags) {
	f := &globalFlags{}

	root := &cobra.Command{
		Use:   "npmctl",
		Short: "Manage Nginx Proxy Manager from the terminal",
		Long: "npmctl wraps the Nginx Proxy Manager v2.15.1 API.\n\n" +
			"Mutations are gated: every write requires BOTH NPMCTL_ALLOW_WRITE=1 in the\n" +
			"environment and --yes on the command line. Use --dry-run to preview any write\n" +
			"without sending a mutating request.\n\n" +
			"Exit codes: 0 ok, 2 usage, 3 refused, 4 auth, 5 not found, 6 api, 7 network\n" +
			"(write may have applied), 8 applied but nginx unhealthy, 9 re-authentication\n" +
			"required.",
		SilenceUsage:      true,
		SilenceErrors:     true,
		DisableAutoGenTag: true,
	}

	p := root.PersistentFlags()
	p.StringVarP(&f.profile, "profile", "p", "", "profile to use (default: configured default_profile)")
	p.StringVar(&f.configPath, "config", "", "path to config.yaml")
	p.StringVar(&f.url, "url", "", "NPM base URL, overriding the profile")
	p.StringVar(&f.token, "token", "", "bearer token to use for this call only (never persisted)")
	p.StringVarP(&f.outputFmt, "output", "o", "", "output format: table, json or yaml (default: table on a terminal, json when piped)")
	p.BoolVarP(&f.verbose, "verbose", "v", false, "log HTTP requests to stderr (secrets are redacted)")
	p.BoolVar(&f.yes, "yes", false, "confirm a mutation (also requires NPMCTL_ALLOW_WRITE=1)")
	p.BoolVar(&f.dryRun, "dry-run", false, "preview a mutation; issues no mutating request")
	p.BoolVar(&f.cascadeAck, "cascade-ack", false, "acknowledge that dependent objects will be affected by a delete")
	p.BoolVar(&f.allowAdvancedConfg, "allow-advanced-config", false, "permit writing raw nginx directives via advanced_config")
	p.BoolVar(&f.force, "force", false, "override a non-safety guard (e.g. the certificate attempt cooldown)")
	p.StringVar(&f.insecure, "insecure", "", "skip TLS verification (true/false); prefer --ca-cert or --pin-sha256")
	p.Lookup("insecure").NoOptDefVal = "true"
	p.StringVar(&f.caCert, "ca-cert", "", "PEM bundle trusted for this instance — solves self-signed without --insecure")
	p.StringVar(&f.pinSHA256, "pin-sha256", "", "expected SHA-256 of the server's public key")
	p.DurationVar(&f.timeout, "timeout", npmapi.DefaultTimeout, "per-request timeout")

	// Cobra reports flag and argument problems as plain errors; the exit-code
	// contract promises 2 for a usage error, so they are typed here.
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return exitcode.Wrap(exitcode.Usage, err, "usage")
	})

	root.AddCommand(
		newVersionCommand(f),
		newAuthCommand(f),
		newHostCommand(f),
		newUndoCommand(f),
		newHealthCommand(f),
		newRedirectCommand(f),
		newStreamCommand(f),
		newDeadHostCommand(f),
		newACLCommand(f),
		newCertCommand(f),
		newAuditLogCommand(f),
		newReportCommand(f),
		newSettingsCommand(f),
		newSchemaCommand(f),
		newDocsCommand(f),
		newSkillCommand(f),
	)
	return root, f
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	root, _ := NewRootCommand()

	// The journal holds unredacted pre-images, so retention cannot depend on the
	// operator remembering to prune. Sweeping on every invocation is the cheapest
	// place to guarantee the 30-day bound.
	sweepJournal()

	if err := root.Execute(); err != nil {
		code := exitcode.Of(err)
		if code == exitcode.Generic && isUsageError(err) {
			code = exitcode.Usage
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return code
	}
	return exitcode.OK
}

// usagePrefixes are the messages cobra produces for a malformed invocation. It
// does not type these errors, so matching the text is the only way to honour the
// exit-code contract for them.
var usagePrefixes = []string{
	"unknown command", "unknown flag", "unknown shorthand flag",
	"invalid argument", "accepts ", "requires at least", "requires exactly",
	"flag needs an argument", "bad flag syntax",
}

func isUsageError(err error) bool {
	msg := err.Error()
	for _, p := range usagePrefixes {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

func sweepJournal() {
	j, err := newJournal()
	if err != nil {
		return
	}
	_, _ = j.Sweep(time.Now())
}
