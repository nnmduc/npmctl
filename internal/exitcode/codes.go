// Package exitcode defines npmctl's stable exit-code contract.
//
// These values are a public interface: the Agent Skill instructs agents to
// branch on them (3 means refused and must not be retried, 9 means a human must
// re-authenticate, 8 means the write applied but nginx is unhealthy). Changing a
// number is a breaking change.
package exitcode

const (
	OK             = 0 // success
	Generic        = 1 // unclassified failure
	Usage          = 2 // bad invocation
	Refused        = 3 // refused, or needs a confirmation that was not given
	Auth           = 4 // credentials rejected
	NotFound       = 5 // resource does not exist
	API            = 6 // NPM returned an error
	Network        = 7 // transport failure — a mutating write MAY have applied
	NginxUnhealthy = 8 // write applied, but nginx reload failed
	ReauthRequired = 9 // interactive re-authentication required
)

// Coded is an error that carries an intended process exit code.
type Coded interface {
	error
	ExitCode() int
}

// Of returns the exit code an error asks for, or Generic.
func Of(err error) int {
	if err == nil {
		return OK
	}
	var c Coded
	if as(err, &c) {
		return c.ExitCode()
	}
	return Generic
}
