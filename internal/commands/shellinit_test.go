package commands

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
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
				`case "$1" in clone|restore)`,
				`cd "$_mws_target"`,
				"export MWS_SHELL_WRAPPER=1",
			},
			mustNotContain:  []string{"function mws", "set -l"},
			funcDeclaration: "mws() {",
		},
		{
			shell: "bash",
			mustContain: []string{
				"MWS_CD_FILE",
				"command mws",
				`case "$1" in clone|restore)`,
				`cd "$_mws_target"`,
				"export MWS_SHELL_WRAPPER=1",
			},
			mustNotContain:  []string{"function mws", "set -l"},
			funcDeclaration: "mws() {",
		},
		{
			shell: "fish",
			mustContain: []string{
				"MWS_CD_FILE",
				"command mws",
				`contains -- "$argv[1]" clone restore`,
				`cd "$_mws_target"`,
				"set -l",
				"set -gx MWS_SHELL_WRAPPER 1",
			},
			mustNotContain:  []string{"mws() {", `case "$1"`},
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

// shellHome points HOME (and ZDOTDIR, unset unless zdotdir is given) at temp
// dirs so no test touches the real user's rc files, and clears the wrapper's
// marker variable. It returns the temp HOME.
func shellHome(t *testing.T, shell, zdotdir string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if zdotdir == "" {
		unsetEnv(t, "ZDOTDIR")
	} else {
		t.Setenv("ZDOTDIR", zdotdir)
	}
	t.Setenv("SHELL", shell)
	unsetEnv(t, "MWS_SHELL_WRAPPER")
	return home
}

func TestShellInitInstallAddsTheMarkedLineOnce(t *testing.T) {
	zdotdir := t.TempDir()
	cases := []struct {
		name, shell, args string
		zdotdir           string
		rc                func(home string) string
		line              string
	}{
		{"zsh", "/bin/zsh", "", "", func(h string) string { return filepath.Join(h, ".zshrc") }, `eval "$(mws shell-init zsh)"`},
		{"zsh with ZDOTDIR", "/usr/bin/zsh", "", zdotdir, func(string) string { return filepath.Join(zdotdir, ".zshrc") }, `eval "$(mws shell-init zsh)"`},
		{"bash", "/bin/bash", "", "", func(h string) string { return filepath.Join(h, ".bashrc") }, `eval "$(mws shell-init bash)"`},
		{"fish", "/usr/local/bin/fish", "", "", func(h string) string { return filepath.Join(h, ".config", "fish", "config.fish") }, "mws shell-init fish | source"},
		{"explicit argument beats $SHELL", "/bin/zsh", "bash", "", func(h string) string { return filepath.Join(h, ".bashrc") }, `eval "$(mws shell-init bash)"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := shellHome(t, tc.shell, tc.zdotdir)
			rc := tc.rc(home)
			// An existing rc without a trailing newline must keep its last line.
			if tc.name == "bash" {
				mustWriteFile(t, rc, "alias ll='ls -l'")
			}
			args := []string{"shell-init", "--install"}
			if tc.args != "" {
				args = []string{"shell-init", tc.args, "--install"}
			}

			res := runMWS(t, home, args...)
			if res.code != 0 {
				t.Fatalf("install: exit %d\nstderr: %s", res.code, res.stderr)
			}
			for _, want := range []string{rc, tc.line, "open a new shell"} {
				if !strings.Contains(res.stderr, want) {
					t.Errorf("install output should name %q, got:\n%s", want, res.stderr)
				}
			}
			first, err := os.ReadFile(rc)
			if err != nil {
				t.Fatalf("read rc: %v", err)
			}
			if n := strings.Count(string(first), tc.line); n != 1 {
				t.Fatalf("rc holds the line %d times, want 1:\n%s", n, first)
			}
			if !strings.Contains(string(first), shellInitMarker) {
				t.Fatalf("rc lacks the marker:\n%s", first)
			}
			if tc.name == "bash" && !strings.HasPrefix(string(first), "alias ll='ls -l'\n") {
				t.Fatalf("existing rc content damaged:\n%s", first)
			}

			res = runMWS(t, home, args...)
			if res.code != 0 {
				t.Fatalf("second install: exit %d\nstderr: %s", res.code, res.stderr)
			}
			if !strings.Contains(res.stderr, "already installed") {
				t.Errorf("second install should say it is already installed, got:\n%s", res.stderr)
			}
			second, err := os.ReadFile(rc)
			if err != nil {
				t.Fatalf("read rc: %v", err)
			}
			if !bytes.Equal(first, second) {
				t.Fatalf("second install changed the rc:\nbefore:\n%s\nafter:\n%s", first, second)
			}
		})
	}
}

func TestRestoreWritesTheCdFile(t *testing.T) {
	meta, wc := setupRmTree(t, &config.Config{ProjectName: "demo"}, "feature")
	if res := runMWS(t, meta, "rm", "feature", "--yes"); res.code != 0 {
		t.Fatalf("rm: exit %d\nstderr: %s", res.code, res.stderr)
	}
	cdFile := filepath.Join(t.TempDir(), "mws-cd")
	t.Setenv("MWS_CD_FILE", cdFile)

	res := runMWS(t, meta, "restore", "feature", "--yes")
	if res.code != 0 {
		t.Fatalf("restore: exit %d\nstderr: %s", res.code, res.stderr)
	}
	got, err := os.ReadFile(cdFile)
	if err != nil {
		t.Fatalf("MWS_CD_FILE not written: %v", err)
	}
	if string(got) != wc {
		t.Fatalf("MWS_CD_FILE holds %q, want %q", got, wc)
	}
}

// TestShellWrapperCdsAfterRestore evals the printed wrapper in a real shell
// and runs restore through it, with this test binary standing in for mws on
// PATH. clone shares the same branch of the wrapper.
func TestShellWrapperCdsAfterRestore(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			shellPath, err := exec.LookPath(shell)
			if err != nil {
				t.Skipf("%s not installed", shell)
			}
			meta, wc := setupRmTree(t, &config.Config{ProjectName: "demo"}, "feature")
			if res := runMWS(t, meta, "rm", "feature", "--yes"); res.code != 0 {
				t.Fatalf("rm: exit %d\nstderr: %s", res.code, res.stderr)
			}
			bin := t.TempDir()
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(self, filepath.Join(bin, "mws")); err != nil {
				t.Fatal(err)
			}
			shellHome(t, shellPath, "")

			script := `eval "$(mws shell-init ` + shell + `)" || exit 9
mws restore feature --yes || exit 8
printf 'cwd=%s\nwrapper=%s\n' "$PWD" "$MWS_SHELL_WRAPPER"`
			sh := exec.Command(shellPath, "-c", script)
			sh.Dir = meta
			sh.Env = append(os.Environ(), cliEnv+"=1", "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			var stdout, stderr bytes.Buffer
			sh.Stdout, sh.Stderr = &stdout, &stderr
			if err := sh.Run(); err != nil {
				t.Fatalf("%s: %v\nstdout: %s\nstderr: %s", shell, err, stdout.String(), stderr.String())
			}
			out := stdout.String()
			if want := "cwd=" + wc + "\n"; !strings.Contains(out, want) {
				t.Errorf("shell cwd after restore should be %s, got:\n%s", wc, out)
			}
			if strings.Contains(out, "wrapper=\n") {
				t.Errorf("the wrapper did not export MWS_SHELL_WRAPPER:\n%s", out)
			}
		})
	}
}

func TestShellInitStatusReportsEachState(t *testing.T) {
	cases := []struct {
		name              string
		installed, loaded bool
		state             string
		code              int
		nextStep          string
	}{
		{"active", true, true, "active", 0, ""},
		{"installed, not loaded", true, false, "installed but not loaded in this shell", 1, "open a new shell"},
		{"not installed", false, false, "not installed", 1, "mws shell-init zsh --install"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := shellHome(t, "/bin/zsh", "")
			if tc.installed {
				if res := runMWS(t, home, "shell-init", "--install"); res.code != 0 {
					t.Fatalf("install: exit %d\nstderr: %s", res.code, res.stderr)
				}
			}
			if tc.loaded {
				t.Setenv("MWS_SHELL_WRAPPER", "1")
			}

			res := runMWS(t, home, "shell-init", "--status")
			if res.code != tc.code {
				t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", res.code, tc.code, res.stdout, res.stderr)
			}
			if got := strings.TrimSpace(res.stdout); got != tc.state {
				t.Errorf("state %q, want %q", got, tc.state)
			}
			if !strings.Contains(res.stderr, tc.nextStep) {
				t.Errorf("stderr should name the next step %q, got:\n%s", tc.nextStep, res.stderr)
			}
		})
	}
}

func TestShellInitWithoutAUsableShellExitsTwo(t *testing.T) {
	for _, env := range []string{"", "/usr/bin/tcsh"} {
		t.Run("SHELL="+env, func(t *testing.T) {
			home := shellHome(t, env, "")
			if env == "" {
				unsetEnv(t, "SHELL")
			}
			res := runMWS(t, home, "shell-init", "--install")
			if res.code != 2 {
				t.Fatalf("exit %d, want 2\nstderr: %s", res.code, res.stderr)
			}
			if !strings.Contains(res.stderr, "mws shell-init <zsh|bash|fish> --install") {
				t.Errorf("stderr should name the invocation to run, got:\n%s", res.stderr)
			}
			if entries, _ := os.ReadDir(home); len(entries) != 0 {
				t.Errorf("a refused install wrote to HOME: %v", entries)
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
