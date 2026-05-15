package commands

import (
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Reporter emits user-facing progress and status messages. Executors call Reporter methods
// instead of writing to stdout directly so the cobra layer owns rendering and tests can swap
// in a recording fake.
type Reporter interface {
	Heading(string)
	OK(string)
	Info(string)
	Warn(string)
	Fail(string)
}

// Styled output primitives. Kept terse on purpose -- the user prefers low-noise CLI output.
var (
	styleHeading = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styleDim     = lipgloss.NewStyle().Faint(true)
	styleOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	styleWarn    = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	styleErr     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

// consoleReporter writes lipgloss-styled lines to an io.Writer (stdout in production).
type consoleReporter struct{ w io.Writer }

func newConsoleReporter() Reporter { return consoleReporter{w: os.Stdout} }

func (r consoleReporter) Heading(s string) { fmt.Fprintln(r.w, styleHeading.Render(s)) }
func (r consoleReporter) OK(s string)      { fmt.Fprintln(r.w, styleOK.Render("[OK] ")+s) }
func (r consoleReporter) Info(s string)    { fmt.Fprintln(r.w, styleDim.Render(s)) }
func (r consoleReporter) Warn(s string)    { fmt.Fprintln(r.w, styleWarn.Render("[WARN] ")+s) }
func (r consoleReporter) Fail(s string)    { fmt.Fprintln(r.w, styleErr.Render("[FAIL] ")+s) }

// nopReporter discards all output. Useful in tests that don't care about UI noise.
type nopReporter struct{}

func (nopReporter) Heading(string) {}
func (nopReporter) OK(string)      {}
func (nopReporter) Info(string)    {}
func (nopReporter) Warn(string)    {}
func (nopReporter) Fail(string)    {}
