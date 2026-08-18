package exitcode

import (
	"errors"
	"fmt"
)

func as(err error, target any) bool { return errors.As(err, target) }

// Error is a plain error with an attached exit code.
type Error struct {
	Code int
	Msg  string
	Err  error
}

func (e *Error) Error() string {
	if e.Err != nil && e.Msg != "" {
		return fmt.Sprintf("%s: %v", e.Msg, e.Err)
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Msg
}
func (e *Error) Unwrap() error { return e.Err }
func (e *Error) ExitCode() int { return e.Code }

// New builds a coded error.
func New(code int, format string, args ...any) *Error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// Wrap attaches a code to an existing error.
func Wrap(code int, err error, format string, args ...any) *Error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, args...), Err: err}
}
