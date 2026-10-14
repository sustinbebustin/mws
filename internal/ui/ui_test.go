package ui

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// plain is what a pipe sees; coloured forces a terminal's colour profile onto
// a buffer, which is how a terminal sees it.
func plain() Styles { return Detect(&bytes.Buffer{}) }

func coloured() Styles {
	r := lipgloss.NewRenderer(&bytes.Buffer{})
	r.SetColorProfile(termenv.TrueColor)
	return newStyles(r)
}

func TestPlainKeepsLegacyText(t *testing.T) {
	s := plain()
	if got := s.Heading("Cloning api ..."); got != "Cloning api ..." {
		t.Errorf("Heading: %q", got)
	}
	if got := s.OK() + " " + s.Warn() + " " + s.Fail(); got != "[OK] [WARN] [FAIL]" {
		t.Errorf("tokens: %q", got)
	}
	got := s.Summary("Planned actions", []KV{
		{Key: "meta workspace", Value: "/tmp/demo"},
		{Key: "working copy", Value: "/tmp/demo/main"},
		{Key: "native repos", Items: []string{"- api  git@github.com:org/api.git"}},
	})
	want := strings.Join([]string{
		"Planned actions",
		"  meta workspace : /tmp/demo",
		"  working copy   : /tmp/demo/main",
		"  native repos   : ",
		"    - api  git@github.com:org/api.git",
	}, "\n")
	if got != want {
		t.Errorf("Summary:\n%s\nwant:\n%s", got, want)
	}
	err := errors.New("no name given\n  mws clone <name>")
	if got, want := s.Error(err), "[FAIL] no name given\n  mws clone <name>"; got != want {
		t.Errorf("Error: %q, want %q", got, want)
	}
}

// Rich output is the plain output plus SGR codes: stripping them must give
// the plain bytes back for every op.
func TestColouredIsPlainPlusSGR(t *testing.T) {
	p, c := plain(), coloured()
	err := errors.New("no name given, and stdin is not a terminal\n  mws clone <name> --setup\n  mws clone <name> --no-setup")
	rows := []KV{{Key: "meta", Value: "/tmp/demo"}, {Key: "repos", Items: []string{"api"}}}
	for name, pair := range map[string][2]string{
		"Heading": {p.Heading("Cloning api ..."), c.Heading("Cloning api ...")},
		"OK":      {p.OK(), c.OK()},
		"Warn":    {p.Warn(), c.Warn()},
		"Fail":    {p.Fail(), c.Fail()},
		"Dim":     {p.Dim("x"), c.Dim("x")},
		"Brand":   {p.Brand("* main"), c.Brand("* main")},
		"Summary": {p.Summary("Plan", rows), c.Summary("Plan", rows)},
		"Error":   {p.Error(err), c.Error(err)},
	} {
		if !strings.Contains(pair[1], "\x1b[") {
			t.Errorf("%s: coloured output has no SGR codes: %q", name, pair[1])
		}
		if got := ansi.Strip(pair[1]); got != pair[0] {
			t.Errorf("%s: stripped coloured output %q != plain %q", name, got, pair[0])
		}
	}
}

func TestTook(t *testing.T) {
	for d, want := range map[time.Duration]string{
		1234 * time.Millisecond: "1.2s",
		47 * time.Millisecond:   "50ms",
		65 * time.Second:        "1m5s",
	} {
		if got := Took(d); got != want {
			t.Errorf("Took(%s) = %q, want %q", d, got, want)
		}
	}
}
