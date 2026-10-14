package commands

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/sustinbebustin/mws/internal/clierr"
	"github.com/sustinbebustin/mws/internal/config"
)

// The headless contract: without a terminal, stdout and stderr carry the same
// bytes they always did, whatever the terminal rendering does.

func TestPlainTrashListIsUnchanged(t *testing.T) {
	noTerminal(t)
	meta, _ := setupRmTree(t, &config.Config{ProjectName: "demo"}, "feature-x")
	var out, errb bytes.Buffer
	withCwd(t, meta, func() {
		if err := runRm(t.Context(), nopReporter{}, "feature-x", rmFlags{yes: true}); err != nil {
			t.Fatalf("rm: %v", err)
		}
		if err := runTrashList(output{stdout: &out, stderr: &errb}); err != nil {
			t.Fatalf("trash list: %v", err)
		}
	})
	if got := out.String(); !strings.HasPrefix(got, "feature-x                trashed less than a minute ago, purges in 7d\n") {
		t.Errorf("trash list stdout:\n%q", got)
	}
	if got := errb.String(); !strings.HasPrefix(got, "Trashed working copies in ") {
		t.Errorf("trash list stderr:\n%q", got)
	}
}

func TestPlainReporterKeepsTokens(t *testing.T) {
	var b bytes.Buffer
	r := reporterFor(&b)
	r.Heading("Cloning api ...")
	r.OK("linked CLAUDE.md")
	r.Done("fast", 300*time.Millisecond)
	r.Done("slow", 4200*time.Millisecond)
	r.Warn("x")
	r.Fail("y")
	r.Info("z")
	want := "Cloning api ...\n[OK] linked CLAUDE.md\n[OK] fast\n[OK] slow (4.2s)\n[WARN] x\n[FAIL] y\nz\n"
	if got := b.String(); got != want {
		t.Errorf("reporter output:\n%q\nwant:\n%q", got, want)
	}
}

func TestPrintErrorIsBareWithoutTerminal(t *testing.T) {
	var b bytes.Buffer
	PrintError(&b, clierr.Usagef("no name given\n  mws clone <name>"))
	if got := b.String(); got != "[FAIL] no name given\n  mws clone <name>\n" {
		t.Errorf("PrintError: %q", got)
	}
}

func TestBareMwsPrintsGroupedHelpWithoutTerminal(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs(nil)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(io.Discard)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("bare mws: %v", err)
	}
	for _, want := range []string{"Workspace:", "Working copies:", "Worktrees:", "  clone ", "  init "} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Errorf("help carries escape codes without a terminal")
	}
}

func TestCancelledPromptExitsOne(t *testing.T) {
	var c *cancelledError
	if err := error(&cancelledError{}); !errors.As(err, &c) || ExitCode(err) != 1 || err.Error() != "Cancelled." {
		t.Errorf("cancelledError: %v, exit %d", err, ExitCode(err))
	}
}
