package worktree

import "github.com/sustinbebustin/mws/internal/clierr"

// The worktree operations tag every error with a clierr.Kind.

func usagef(format string, args ...any) error   { return clierr.Usagef(format, args...) }
func refusedf(format string, args ...any) error { return clierr.Refusedf(format, args...) }
func failuref(format string, args ...any) error { return clierr.Failuref(format, args...) }

// wrapKind rewords err while keeping its Kind (Failure when untagged).
func wrapKind(err error, format string, args ...any) error {
	return clierr.Wrapf(err, format, args...)
}
