// Package clierr is the mws CLI's error taxonomy: every error a command
// returns maps to one Kind, which fixes its process exit code and its code in
// --json output.
package clierr

import (
	"errors"
	"fmt"
)

// Kind classifies an error by how a caller should react to it.
type Kind int

const (
	// Failure is an unexpected git or filesystem error.
	Failure Kind = iota
	// Usage is a bad name, flag, ref or invocation context, or an input that
	// would need a prompt when there is no terminal.
	Usage
	// Refused means a safety invariant blocked the operation.
	Refused
	// NotFound means the named working copy, trash entry or worktree does not
	// exist, e.g. on a retried removal.
	NotFound
)

// ExitCode is the process exit code for k: 1 failure, 2 usage, 3 refused,
// 4 not found.
func (k Kind) ExitCode() int {
	switch k {
	case Usage:
		return 2
	case Refused:
		return 3
	case NotFound:
		return 4
	default:
		return 1
	}
}

// Code names k in machine-readable output: "failure", "usage", "refused" or
// "not_found".
func (k Kind) Code() string {
	switch k {
	case Usage:
		return "usage"
	case Refused:
		return "refused"
	case NotFound:
		return "not_found"
	default:
		return "failure"
	}
}

// Error is a command failure tagged with its Kind.
type Error struct {
	Kind Kind
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// ExitCode is the process exit code for the error's Kind.
func (e *Error) ExitCode() int { return e.Kind.ExitCode() }

// Usagef returns a Usage error formatted like fmt.Errorf.
func Usagef(format string, args ...any) error { return newf(Usage, format, args...) }

// Refusedf returns a Refused error formatted like fmt.Errorf.
func Refusedf(format string, args ...any) error { return newf(Refused, format, args...) }

// Failuref returns a Failure error formatted like fmt.Errorf.
func Failuref(format string, args ...any) error { return newf(Failure, format, args...) }

// NotFoundf returns a NotFound error formatted like fmt.Errorf.
func NotFoundf(format string, args ...any) error { return newf(NotFound, format, args...) }

func newf(k Kind, format string, args ...any) error {
	return &Error{Kind: k, Err: fmt.Errorf(format, args...)}
}

// KindOf returns the Kind of the first tagged error in err's tree, Failure
// when there is none.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return Failure
}

// Wrapf rewords err while keeping its Kind (Failure when untagged).
func Wrapf(err error, format string, args ...any) error {
	return newf(KindOf(err), format, args...)
}

// Join joins errs into one error whose Kind is the most severe among them:
// any untagged or Failure error makes the whole a Failure, so a real failure
// is never reported as a usage mistake. It returns nil when errs are all nil.
func Join(errs ...error) error {
	joined := errors.Join(errs...)
	if joined == nil {
		return nil
	}
	kind, first := Failure, true
	for _, err := range errs {
		if err == nil {
			continue
		}
		k := KindOf(err)
		switch {
		case k == Failure:
			return &Error{Kind: Failure, Err: joined}
		case first:
			kind, first = k, false
		case k != kind:
			return &Error{Kind: Failure, Err: joined}
		}
	}
	return &Error{Kind: kind, Err: joined}
}
