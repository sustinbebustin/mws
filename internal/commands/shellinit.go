package commands

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// Shell wrappers below detect `mws clone` and `mws restore` and route them
// through a tempfile-based handoff: the binary writes the working copy's path
// to $MWS_CD_FILE, the wrapper reads it and cds the parent shell. Every other
// subcommand is a pure passthrough via `command mws`. The match is on $1 --
// mws has no global flags today, so this is sufficient. Each wrapper exports
// MWS_SHELL_WRAPPER once defined, which `shell-init --status` reads.

const shellInitPOSIX = `# mws shell integration -- add it with ` + "`mws shell-init --install`" + `, or eval at shell start:
#   eval "$(mws shell-init zsh)"
#   eval "$(mws shell-init bash)"
mws() {
  case "$1" in clone|restore)
    local _mws_cd_file _mws_rc _mws_target
    _mws_cd_file=$(mktemp -t mws-cd.XXXXXX) || { command mws "$@"; return $?; }
    MWS_CD_FILE="$_mws_cd_file" command mws "$@"
    _mws_rc=$?
    if [ $_mws_rc -eq 0 ] && [ -s "$_mws_cd_file" ]; then
      _mws_target=$(cat "$_mws_cd_file")
      [ -d "$_mws_target" ] && cd "$_mws_target"
    fi
    rm -f "$_mws_cd_file"
    return $_mws_rc
    ;;
  esac
  command mws "$@"
}
export MWS_SHELL_WRAPPER=1
`

const shellInitFish = `# mws shell integration -- add it with ` + "`mws shell-init --install`" + `, or at shell start:
#   mws shell-init fish | source
function mws
  if test (count $argv) -gt 0; and contains -- "$argv[1]" clone restore
    set -l _mws_cd_file (mktemp -t mws-cd.XXXXXX)
    if test -z "$_mws_cd_file"
      command mws $argv
      return $status
    end
    MWS_CD_FILE=$_mws_cd_file command mws $argv
    set -l _mws_rc $status
    if test $_mws_rc -eq 0 -a -s "$_mws_cd_file"
      set -l _mws_target (cat $_mws_cd_file)
      if test -d "$_mws_target"
        cd "$_mws_target"
      end
    end
    rm -f $_mws_cd_file
    return $_mws_rc
  end
  command mws $argv
end
set -gx MWS_SHELL_WRAPPER 1
`

// shellInitMarker opens the block --install appends to an rc file; finding it
// is how a second --install (and --status) knows the line is there.
const (
	shellInitMarker    = "# >>> mws shell-init >>>"
	shellInitMarkerEnd = "# <<< mws shell-init <<<"
)

// shellSpec is one supported shell: the wrapper it evals, the line that loads
// it at startup, and the rc file that line goes in.
type shellSpec struct {
	name    string
	wrapper string
	line    string
	rcFile  func(home string) string
}

var supportedShells = []shellSpec{
	{
		name:    "zsh",
		wrapper: shellInitPOSIX,
		line:    `eval "$(mws shell-init zsh)"`,
		rcFile: func(home string) string {
			if dir := os.Getenv("ZDOTDIR"); dir != "" {
				return filepath.Join(dir, ".zshrc")
			}
			return filepath.Join(home, ".zshrc")
		},
	},
	{
		name:    "bash",
		wrapper: shellInitPOSIX,
		line:    `eval "$(mws shell-init bash)"`,
		rcFile:  func(home string) string { return filepath.Join(home, ".bashrc") },
	},
	{
		name:    "fish",
		wrapper: shellInitFish,
		line:    "mws shell-init fish | source",
		rcFile:  func(home string) string { return filepath.Join(home, ".config", "fish", "config.fish") },
	},
}

func lookupShell(name string) (shellSpec, bool) {
	for _, s := range supportedShells {
		if s.name == name {
			return s, true
		}
	}
	return shellSpec{}, false
}

func newShellInitCmd() *cobra.Command {
	var install, status bool
	cmd := &cobra.Command{
		Use:   "shell-init [shell]",
		Short: "Print, install or check the shell function that cds into a copy after `mws clone` or `mws restore`",
		Long: `shell-init prints a shell function definition that wraps the mws binary.
With the wrapper loaded, ` + "`mws clone <name>`" + ` and ` + "`mws restore <name>`" + ` cd the
parent shell into the working copy on success. Every other subcommand is a pure
passthrough. The shell defaults to the one $SHELL names.

--install adds one line that loads the wrapper to the shell's rc file
(~/.zshrc, or $ZDOTDIR/.zshrc; ~/.bashrc; ~/.config/fish/config.fish), between
marker comments, so running it again changes nothing. Open a new shell after.

--status prints "active" (exit 0), "installed but not loaded in this shell"
(exit 1) or "not installed" (exit 1), with the next step on stderr.

Without the wrapper, ` + "`mws clone`" + ` still prints the new working copy's path
as its last stdout line -- a child process cannot change its parent shell's
working directory, so cd there yourself.

The wrapper invokes the binary with MWS_CD_FILE pointing at a tempfile; the
binary writes the working copy's absolute path there on success. External
tools (IDEs, custom scripts) can use the same MWS_CD_FILE contract directly.`,
		Example: `  mws shell-init --install
  mws shell-init --status
  eval "$(mws shell-init zsh)"
  mws shell-init fish | source`,
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := exclusiveFlags(cmd, "install", "status"); err != nil {
				return err
			}
			flag := ""
			switch {
			case install:
				flag = " --install"
			case status:
				flag = " --status"
			}
			spec, err := resolveShell(args, flag)
			if err != nil {
				return err
			}
			switch {
			case install:
				return installShellInit(cmd, spec)
			case status:
				return shellInitStatus(cmd, spec)
			}
			fmt.Fprint(cmd.OutOrStdout(), spec.wrapper)
			return nil
		},
	}
	cmd.Flags().BoolVar(&install, "install", false, "add the line that loads the wrapper to the shell's rc file")
	cmd.Flags().BoolVar(&status, "status", false, "report whether the wrapper is active in this shell")
	return cmd
}

// resolveShell picks the shell from the argument, else from the one $SHELL
// names. flag is the mode flag to repeat in the hint, e.g. " --install".
func resolveShell(args []string, flag string) (shellSpec, error) {
	if len(args) == 1 {
		if spec, ok := lookupShell(args[0]); ok {
			return spec, nil
		}
		return shellSpec{}, usagef("unsupported shell %q (supported: zsh, bash, fish), e.g. mws shell-init zsh%s", args[0], flag)
	}
	env := os.Getenv("SHELL")
	if spec, ok := lookupShell(filepath.Base(env)); ok && env != "" {
		return spec, nil
	}
	reason := "$SHELL is not set"
	if env != "" {
		reason = fmt.Sprintf("$SHELL is %s, which is not a supported shell", env)
	}
	return shellSpec{}, usagef("%s; name the shell: mws shell-init <zsh|bash|fish>%s", reason, flag)
}

// installShellInit appends the marked line to spec's rc file, or leaves the
// file untouched when the marker is already there.
func installShellInit(cmd *cobra.Command, spec shellSpec) error {
	r := reporterFor(cmd.ErrOrStderr())
	rc, err := rcPath(spec)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(rc)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read %s to check for the mws shell-init line: %w", rc, err)
	}
	if bytes.Contains(body, []byte(shellInitMarker)) {
		r.OK(fmt.Sprintf("mws shell-init is already installed in %s; nothing changed", rc))
		return nil
	}

	var block strings.Builder
	if len(body) > 0 && !bytes.HasSuffix(body, []byte("\n")) {
		block.WriteString("\n")
	}
	block.WriteString(shellInitMarker + "\n" + spec.line + "\n" + shellInitMarkerEnd + "\n")
	if err := os.MkdirAll(filepath.Dir(rc), 0o755); err != nil {
		return fmt.Errorf("create %s for the mws shell-init line: %w", filepath.Dir(rc), err)
	}
	f, err := os.OpenFile(rc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s to add the mws shell-init line: %w", rc, err)
	}
	if _, err := f.WriteString(block.String()); err != nil {
		_ = f.Close()
		return fmt.Errorf("append the mws shell-init line to %s: %w; check the end of the file before retrying", rc, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s after adding the mws shell-init line: %w", rc, err)
	}
	r.OK(fmt.Sprintf("Added to %s: %s", rc, spec.line))
	r.Info("open a new shell (or run the line above) to load the mws wrapper")
	return nil
}

// shellWrapperEnv is exported by the wrapper when it is defined, so --status
// can tell a loaded wrapper from a line that only sits in the rc file.
const shellWrapperEnv = "MWS_SHELL_WRAPPER"

// shellInitStatus prints whether the wrapper is active, installed but not
// loaded, or not installed, exiting 1 with the next step for the latter two.
func shellInitStatus(cmd *cobra.Command, spec shellSpec) error {
	if os.Getenv(shellWrapperEnv) != "" {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "active")
		return err
	}
	rc, err := rcPath(spec)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(rc)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read %s to check for the mws shell-init line: %w", rc, err)
	}
	r := reporterFor(cmd.ErrOrStderr())
	if bytes.Contains(body, []byte(shellInitMarker)) {
		fmt.Fprintln(cmd.OutOrStdout(), "installed but not loaded in this shell")
		r.Info(fmt.Sprintf("%s loads the mws wrapper; open a new shell, or run: %s", rc, spec.line))
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "not installed")
		r.Info(fmt.Sprintf("run `mws shell-init %s --install` to add the wrapper to %s", spec.name, rc))
	}
	return &exitStatusError{code: 1}
}

func rcPath(spec shellSpec) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find the home directory for the %s rc file: %w; set $HOME", spec.name, err)
	}
	return spec.rcFile(home), nil
}
