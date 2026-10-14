// Package ui colours mws's terminal output. A Styles value is bound to one
// stream: on a terminal its ops add colour and weight, elsewhere they return
// the text unchanged, so piped output and agents see exactly what they always
// did. The invariant is "rich = plain + SGR codes", and nothing here changes
// layout or content. Colour follows the terminal's own ANSI palette, so it
// fits the user's theme, and NO_COLOR turns it off.
package ui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// ANSI colours: blue for headings and the current entry, green, yellow and red
// for the status tokens. Faint is the terminal's own secondary tone.
var (
	colorBrand = lipgloss.Color("12")
	colorOK    = lipgloss.Color("10")
	colorWarn  = lipgloss.Color("11")
	colorFail  = lipgloss.Color("9")
)

// Styles colours output for one stream.
type Styles struct {
	brand, bold, dim, ok, warn, fail lipgloss.Style
}

// Detect binds Styles to w: coloured when w is a terminal, plain otherwise.
func Detect(w io.Writer) Styles {
	r := lipgloss.NewRenderer(w)
	// The palette is the terminal's own, so never ask it about its background
	// (the query stalls or leaks where nothing answers it).
	r.SetHasDarkBackground(true)
	return newStyles(r)
}

func newStyles(r *lipgloss.Renderer) Styles {
	return Styles{
		brand: r.NewStyle().Foreground(colorBrand).Bold(true),
		bold:  r.NewStyle().Bold(true),
		dim:   r.NewStyle().Faint(true),
		ok:    r.NewStyle().Foreground(colorOK).Bold(true),
		warn:  r.NewStyle().Foreground(colorWarn).Bold(true),
		fail:  r.NewStyle().Foreground(colorFail).Bold(true),
	}
}

// Status tokens. The text never changes, so output greps the same in every
// mode.
func (s Styles) OK() string   { return s.ok.Render("[OK]") }
func (s Styles) Warn() string { return s.warn.Render("[WARN]") }
func (s Styles) Fail() string { return s.fail.Render("[FAIL]") }

// Heading renders a section title.
func (s Styles) Heading(text string) string { return s.brand.Render(text) }

// Brand renders the entry that matters, such as the current working copy.
func (s Styles) Brand(text string) string { return s.brand.Render(text) }

// Bold renders emphasised text.
func (s Styles) Bold(text string) string { return s.bold.Render(text) }

// Dim renders secondary text.
func (s Styles) Dim(text string) string { return s.dim.Render(text) }

// KV is one row of a Summary. Items, when set, are listed under the key
// instead of Value.
type KV struct {
	Key   string
	Value string
	Items []string
}

// Summary renders a titled key/value block, keys aligned and dim.
func (s Styles) Summary(title string, rows []KV) string {
	width := 0
	for _, kv := range rows {
		width = max(width, len(kv.Key))
	}
	var b strings.Builder
	b.WriteString(s.Heading(title))
	for _, kv := range rows {
		fmt.Fprintf(&b, "\n  %s : %s", s.dim.Render(fmt.Sprintf("%-*s", width, kv.Key)), kv.Value)
		for _, it := range kv.Items {
			fmt.Fprintf(&b, "\n    %s", it)
		}
	}
	return b.String()
}

// Error renders a command's final error: a [FAIL] token before the message,
// with each "mws ..." invocation on a continuation line in bold so the fix is
// the first thing the eye lands on.
func (s Styles) Error(err error) string {
	lines := strings.Split(err.Error(), "\n")
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s", s.Fail(), lines[0])
	for _, l := range lines[1:] {
		if text := strings.TrimLeft(l, " "); strings.HasPrefix(text, "mws ") {
			l = l[:len(l)-len(text)] + s.bold.Render(text)
		}
		fmt.Fprintf(&b, "\n%s", l)
	}
	return b.String()
}

// Took renders a step's duration for a status line, e.g. "4.2s" or "1m5s".
func Took(d time.Duration) string {
	switch {
	case d < time.Second:
		return d.Round(10 * time.Millisecond).String()
	case d < time.Minute:
		return d.Round(100 * time.Millisecond).String()
	default:
		return d.Round(time.Second).String()
	}
}

// FormTheme is the huh theme for mws prompts: the base theme with the brand
// colour on the focused field's edge, selectors and buttons.
func FormTheme() *huh.Theme {
	t := huh.ThemeBase()
	t.Focused.Base = t.Focused.Base.BorderForeground(colorBrand)
	t.Focused.Title = t.Focused.Title.Bold(true)
	t.Focused.Description = t.Focused.Description.Faint(true)
	t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.Foreground(colorFail)
	t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(colorFail)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(colorBrand)
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(colorBrand)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(colorBrand)
	t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.Foreground(colorOK)
	t.Focused.FocusedButton = t.Focused.FocusedButton.Foreground(lipgloss.Color("0")).Background(colorBrand).Bold(true)
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(colorBrand)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(colorBrand)
	t.Blurred.Title = t.Blurred.Title.Faint(true)
	t.Blurred.Description = t.Blurred.Description.Faint(true)
	t.Help.ShortKey = t.Help.ShortKey.Foreground(colorBrand)
	t.Help.FullKey = t.Help.FullKey.Foreground(colorBrand)
	return t
}
