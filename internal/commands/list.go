package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List working copies in this meta workspace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(newConsoleReporter())
		},
	}
}

func runList(r Reporter) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ws, err := project.Locate(cwd)
	if err != nil {
		return err
	}
	// Locate tolerates a malformed .mws.toml so it can still report the meta
	// root; surface the real parse error here before scanning the (possibly
	// wrong) copies root.
	if _, err := config.Load(ws.MetaRoot); err != nil {
		return err
	}

	r.Heading(fmt.Sprintf("Meta workspace: %s", ws.MetaRoot))
	peers, err := ws.EnumerateCopies()
	if err != nil {
		return err
	}
	if len(peers) == 0 {
		r.Info("No working copies.")
		return nil
	}
	for _, peer := range peers {
		marker := "  "
		if peer == ws.WorkingCopy {
			marker = "* "
		}
		r.Info(marker + filepath.Base(peer))
	}
	return nil
}
