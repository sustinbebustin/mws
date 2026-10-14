package commands

import (
	"errors"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/x/term"

	"github.com/sustinbebustin/mws/internal/ui"
)

// canPrompt reports whether mws may ask the user: stdin is a terminal. Agents,
// scripts and tests run without one, so every prompt site checks it first and
// otherwise fails with noPrompt, naming the flag or argument to pass instead.
var canPrompt = func() bool { return term.IsTerminal(os.Stdin.Fd()) }

// noPrompt is the error for an input mws would prompt for when stdin is not a
// terminal: missing says what was not given, and each of try is a complete
// invocation that supplies it.
func noPrompt(missing string, try ...string) error {
	return usagef("%s, and stdin is not a terminal to prompt on. Pass it on the command line:\n  %s", missing, strings.Join(try, "\n  "))
}

// cancelledError is a prompt the user left with ctrl-c or esc. It exits 1 with
// a short message instead of huh's "user aborted".
type cancelledError struct{}

func (*cancelledError) Error() string { return "Cancelled." }
func (*cancelledError) ExitCode() int { return 1 }

// runForm runs the groups as one themed form on stderr, with the key help
// shown. Every prompt goes through here (or runField) so they all look alike.
func runForm(groups ...*huh.Group) error {
	err := huh.NewForm(groups...).
		WithTheme(ui.FormTheme()).
		WithShowHelp(true).
		WithOutput(os.Stderr).
		Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return &cancelledError{}
	}
	return err
}

// runField runs one field as a form of its own.
func runField(f huh.Field) error { return runForm(huh.NewGroup(f)) }

// confirmFlags is the --yes / --dry-run pair of a command that asks before a
// destructive step.
type confirmFlags struct {
	yes, dryRun bool
}

// confirmation describes the yes/no question before a destructive step.
type confirmation struct {
	title, description, affirmative string
	// missing and try build the noPrompt error when there is no terminal.
	missing string
	try     []string
}

// confirm reports whether to go ahead with c. yes answers it without asking;
// without a terminal it is a usage error; a declined prompt prints
// "Cancelled." and returns false.
func confirm(r Reporter, yes bool, c confirmation) (bool, error) {
	if yes {
		return true, nil
	}
	if !canPrompt() {
		return false, noPrompt(c.missing, c.try...)
	}
	var ok bool
	if err := runField(huh.NewConfirm().
		Title(c.title).
		Description(c.description).
		Affirmative(c.affirmative).
		Negative("Cancel").
		Value(&ok)); err != nil {
		return false, err
	}
	if !ok {
		r.Warn("Cancelled.")
	}
	return ok, nil
}
