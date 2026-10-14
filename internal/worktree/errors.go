package worktree

import (
	"errors"
	"fmt"
)

// Kind classifies a worktree error by how a caller should react to it.
type Kind int

const (
	// Failure is an unexpected git or filesystem error.
	Failure Kind = iota
	// Usage is a bad name, flag, ref or invocation context.
	Usage
	// Refused means a safety invariant blocked the operation.
	Refused
)

// ExitCode is the process exit code for k: 1 failure, 2 usage, 3 refused.
func (k Kind) ExitCode() int {
	switch k {
	case Usage:
		return 2
	case Refused:
		return 3
	default:
		return 1
	}
}

// Code names k in machine-readable output: "failure", "usage" or "refused".
func (k Kind) Code() string {
	switch k {
	case Usage:
		return "usage"
	case Refused:
		return "refused"
	default:
		return "failure"
	}
}

// Error is a worktree operation failure tagged with its Kind.
type Error struct {
	Kind Kind
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// ExitCode is the process exit code for the error's Kind.
func (e *Error) ExitCode() int { return e.Kind.ExitCode() }

func usagef(format string, args ...any) error {
	return &Error{Kind: Usage, Err: fmt.Errorf(format, args...)}
}

func refusedf(format string, args ...any) error {
	return &Error{Kind: Refused, Err: fmt.Errorf(format, args...)}
}

func failuref(format string, args ...any) error {
	return &Error{Kind: Failure, Err: fmt.Errorf(format, args...)}
}

// wrapKind rewords err while keeping its Kind (Failure when untagged).
func wrapKind(err error, format string, args ...any) error {
	kind := Failure
	var we *Error
	if errors.As(err, &we) {
		kind = we.Kind
	}
	return &Error{Kind: kind, Err: fmt.Errorf(format, args...)}
}
