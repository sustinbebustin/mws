package commands

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newPathCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "path [copy]",
		Short: "Print the absolute path of a working copy",
		Long: `path prints the absolute path of a working copy on stdout. Without a name it
uses the working copy containing the current directory. A name that is no
working copy exits 4 (not found). --json prints {"copy", "path"}.`,
		Example: `  mws path feature-x
  cd "$(mws path feature-x)"
  mws path feature-x --json`,
		Args: usageCheckedArgs(cobra.MaximumNArgs(1), &jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPath(newOutput(cmd, jsonOut), copyArg(args))
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print the copy and path as JSON")
	return cmd
}

func runPath(o output, name string) error {
	ws, _, err := o.locate()
	if err != nil {
		return err
	}
	copyPath, err := resolveCopy(ws, name, "mws path <copy>")
	if err != nil {
		return o.fail(err)
	}
	return o.result(struct {
		Copy string `json:"copy"`
		Path string `json:"path"`
	}{filepath.Base(copyPath), copyPath}, func(w io.Writer) error {
		_, err := fmt.Fprintln(w, copyPath)
		return err
	})
}
