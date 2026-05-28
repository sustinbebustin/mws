package commands

import (
	"bytes"
	"strings"
	"testing"
)

func TestShellInitEmitsWrapperPerShell(t *testing.T) {
	cases := []struct {
		shell           string
		mustContain     []string
		mustNotContain  []string
		funcDeclaration string
	}{
		{
			shell: "zsh",
			mustContain: []string{
				"MWS_CD_FILE",
				"command mws",
				`"$1" = "clone"`,
				`cd "$_mws_target"`,
			},
			mustNotContain:  []string{"function mws", "set -l"},
			funcDeclaration: "mws() {",
		},
		{
			shell: "bash",
			mustContain: []string{
				"MWS_CD_FILE",
				"command mws",
				`"$1" = "clone"`,
				`cd "$_mws_target"`,
			},
			mustNotContain:  []string{"function mws", "set -l"},
			funcDeclaration: "mws() {",
		},
		{
			shell: "fish",
			mustContain: []string{
				"MWS_CD_FILE",
				"command mws",
				`"$argv[1]" = "clone"`,
				`cd "$_mws_target"`,
				"set -l",
			},
			mustNotContain:  []string{"mws() {", `"$1" = "clone"`},
			funcDeclaration: "function mws",
		},
	}

	for _, tc := range cases {
		t.Run(tc.shell, func(t *testing.T) {
			cmd := newShellInitCmd()
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			cmd.SetArgs([]string{tc.shell})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("execute: %v", err)
			}
			out := buf.String()
			if !strings.Contains(out, tc.funcDeclaration) {
				t.Errorf("missing function declaration %q in:\n%s", tc.funcDeclaration, out)
			}
			for _, s := range tc.mustContain {
				if !strings.Contains(out, s) {
					t.Errorf("missing %q in:\n%s", s, out)
				}
			}
			for _, s := range tc.mustNotContain {
				if strings.Contains(out, s) {
					t.Errorf("unexpected %q in:\n%s", s, out)
				}
			}
		})
	}
}

func TestShellInitRejectsUnknownShell(t *testing.T) {
	cmd := newShellInitCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"powershell"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unsupported shell")
	}
	if !strings.Contains(err.Error(), "powershell") {
		t.Errorf("error should name the shell, got: %v", err)
	}
}
