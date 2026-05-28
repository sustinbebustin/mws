package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Shell wrappers below detect `mws clone` and route it through a tempfile-based
// handoff: the binary writes the new working copy's path to $MWS_CD_FILE, the
// wrapper reads it and cds the parent shell. Every other subcommand is a pure
// passthrough via `command mws`. The match is on $1 == "clone" -- mws has no
// global flags today, so this is sufficient.

const shellInitPOSIX = `# mws shell integration -- paste into ~/.zshrc or ~/.bashrc, or eval at shell start:
#   eval "$(mws shell-init zsh)"
#   eval "$(mws shell-init bash)"
mws() {
  if [ "$1" = "clone" ]; then
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
  fi
  command mws "$@"
}
`

const shellInitFish = `# mws shell integration -- add to ~/.config/fish/config.fish:
#   mws shell-init fish | source
function mws
  if test (count $argv) -gt 0; and test "$argv[1]" = "clone"
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
`

func newShellInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell-init <zsh|bash|fish>",
		Short: "Print a shell function for `eval` that auto-cds into the new working copy after `mws clone`",
		Long: `shell-init prints a shell function definition that wraps the mws binary.
With the wrapper installed, ` + "`mws clone <name>`" + ` automatically cds the
parent shell into the new working copy on success. Every other subcommand is a
pure passthrough.

Install (one-time):

  # zsh
  eval "$(mws shell-init zsh)"        # add to ~/.zshrc

  # bash
  eval "$(mws shell-init bash)"       # add to ~/.bashrc

  # fish
  mws shell-init fish | source         # add to ~/.config/fish/config.fish

Without the wrapper, ` + "`mws clone`" + ` prints a copy-pasteable ` + "`cd <path>`" + ` hint
instead -- a child process cannot change its parent shell's working directory.

The wrapper invokes the binary with MWS_CD_FILE pointing at a tempfile; the
binary writes the new working copy's absolute path there on success. External
tools (IDEs, custom scripts) can use the same MWS_CD_FILE contract directly.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "zsh", "bash":
				fmt.Fprint(cmd.OutOrStdout(), shellInitPOSIX)
			case "fish":
				fmt.Fprint(cmd.OutOrStdout(), shellInitFish)
			default:
				return fmt.Errorf("unsupported shell %q (supported: zsh, bash, fish)", args[0])
			}
			return nil
		},
	}
}
