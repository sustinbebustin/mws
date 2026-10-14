package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Populated at build time via -ldflags by goreleaser. Defaults indicate a
// local dev build (`go build` / `go install` without ldflags).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print mws version, commit, and build date",
		Long: `version prints three lines on stdout: "mws <version>", "commit: <sha>" and
"built:  <date>".`,
		Example: `  mws version
  mws version | head -n1`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "mws %s\ncommit: %s\nbuilt:  %s\n", version, commit, date)
		},
	}
}
