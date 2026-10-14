package commands

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sustinbebustin/mws/internal/ui"
)

// Reporter emits user-facing progress and status messages. Executors call Reporter methods
// instead of writing to stdout directly so the cobra layer owns rendering and tests can swap
// in a recording fake.
type Reporter interface {
	Heading(string)
	OK(string)
	// Done is OK for a step that may take a while; the duration is shown
	// once it is worth knowing.
	Done(string, time.Duration)
	Info(string)
	Warn(string)
	Fail(string)
	// Summary shows a titled key/value plan before a confirmation.
	Summary(string, []ui.KV)
}

// Styled output primitives. Kept terse on purpose -- the user prefers
// low-noise CLI output. Colour follows the writer: on when it is a terminal,
// off elsewhere, with the text the same either way.
type consoleReporter struct {
	w io.Writer
	s ui.Styles
}

// showTimingFrom is the step duration from which Done appends it; faster
// steps need no number.
const showTimingFrom = time.Second

func newConsoleReporter() Reporter { return reporterFor(os.Stderr) }

func reporterFor(w io.Writer) consoleReporter { return consoleReporter{w: w, s: ui.Detect(w)} }

func (r consoleReporter) Heading(s string) { fmt.Fprintln(r.w, r.s.Heading(s)) }
func (r consoleReporter) OK(s string)      { fmt.Fprintln(r.w, r.s.OK()+" "+s) }
func (r consoleReporter) Done(s string, took time.Duration) {
	if took < showTimingFrom {
		r.OK(s)
		return
	}
	fmt.Fprintln(r.w, r.s.OK()+" "+s+" "+r.s.Dim("("+ui.Took(took)+")"))
}
func (r consoleReporter) Info(s string) { fmt.Fprintln(r.w, r.s.Dim(s)) }
func (r consoleReporter) Warn(s string) { fmt.Fprintln(r.w, r.s.Warn()+" "+s) }
func (r consoleReporter) Fail(s string) { fmt.Fprintln(r.w, r.s.Fail()+" "+s) }
func (r consoleReporter) Summary(title string, rows []ui.KV) {
	fmt.Fprintln(r.w, "\n"+r.s.Summary(title, rows))
}

// nopReporter discards all output. Useful in tests that don't care about UI noise.
type nopReporter struct{}

func (nopReporter) Heading(string)             {}
func (nopReporter) OK(string)                  {}
func (nopReporter) Done(string, time.Duration) {}
func (nopReporter) Info(string)                {}
func (nopReporter) Warn(string)                {}
func (nopReporter) Fail(string)                {}
func (nopReporter) Summary(string, []ui.KV)    {}
