// Package cli wires npmctl's commands together.
package cli

import (
	"io"
	"os"
	"time"

	"github.com/nnmduc/npmctl/internal/auth"
	"github.com/nnmduc/npmctl/internal/config"
	"github.com/nnmduc/npmctl/internal/exitcode"
	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/nnmduc/npmctl/internal/undo"
)

// globalFlags are the flags every command shares.
type globalFlags struct {
	profile    string
	configPath string
	url        string
	token      string
	outputFmt  string
	verbose    bool

	// Write-gate flags. yes is only ONE of the two required factors; the other,
	// NPMCTL_ALLOW_WRITE=1, lives outside argv on purpose.
	yes                bool
	dryRun             bool
	cascadeAck         bool
	allowAdvancedConfg bool
	force              bool

	insecure  string // tri-state: "" inherits the profile, "true"/"false" override
	caCert    string
	pinSHA256 string
	timeout   time.Duration
}

// Process streams, indirected so tests can drive the command tree end to end
// and assert on exactly what a user would see.

// Process streams, indirected so tests can drive the command tree end to end
// and assert on exactly what a user would see.
var (
	outWriter io.Writer = os.Stdout
	errWriter io.Writer = os.Stderr
	inReader  io.Reader = os.Stdin
)

// SetStreams redirects npmctl's streams. Intended for tests.

// SetStreams redirects npmctl's streams. Intended for tests.
func SetStreams(out, err io.Writer, in io.Reader) func() {
	po, pe, pi := outWriter, errWriter, inReader
	outWriter, errWriter, inReader = out, err, in
	return func() { outWriter, errWriter, inReader = po, pe, pi }
}

// runtime is everything a command needs at execution time.

// runtime is everything a command needs at execution time.
type runtime struct {
	flags *globalFlags

	cfg         *config.Config
	profileName string
	profile     *config.Profile

	stdout io.Writer
	stderr io.Writer
	stdin  io.Reader

	format  output.Format
	journal *undo.Journal

	// client is built lazily: `npmctl version` and `auth login` must work before
	// any credential exists.
	client   *npmapi.Client
	resolved *auth.Resolved
}

// newRuntime loads configuration and resolves the active profile. It performs no
// network I/O and requires no credential.

// newRuntime loads configuration and resolves the active profile. It performs no
// network I/O and requires no credential.
func newRuntime(f *globalFlags) (*runtime, error) {
	cfg, err := config.Load(f.configPath)
	if err != nil {
		return nil, err
	}
	name := cfg.ResolveName(f.profile, os.Getenv(auth.EnvProfile))
	prof := cfg.Get(name)

	// An explicit --url or NPMCTL_URL overrides the stored profile URL, which is
	// how a one-shot call against an unconfigured instance works.
	if f.url != "" {
		prof.URL = f.url
	} else if env := os.Getenv(auth.EnvURL); env != "" && prof.URL == "" {
		prof.URL = env
	}
	if env := os.Getenv(auth.EnvIdentity); env != "" && prof.Identity == "" {
		prof.Identity = env
	}

	fmtOverride, err := output.ParseFormat(f.outputFmt)
	if err != nil {
		return nil, exitcode.Wrap(exitcode.Usage, err, "invalid -o value")
	}

	j, err := undo.New()
	if err != nil {
		return nil, err
	}

	rt := &runtime{
		flags:       f,
		cfg:         cfg,
		profileName: name,
		profile:     prof,
		stdout:      outWriter,
		stderr:      errWriter,
		stdin:       inReader,
		journal:     j,
	}
	rt.format = output.Resolve(fmtOverride, rt.stdout)
	return rt, nil
}

// insecureSkipVerify resolves the tri-state --insecure against the profile.
