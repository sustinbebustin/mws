package commands

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
)

func newRelinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "relink",
		Short: "Refresh harness symlinks in every working copy",
		Long: `relink walks every working copy under this meta workspace and re-runs
symlink discovery against the harness. New top-level harness entries get linked
in, broken symlinks are repaired, and existing non-symlink files are left alone.

If a working copy holds a regular file where the harness has the same-named
regular file (typically because an editor's atomic-save replaced a symlink),
relink detects the divergence. Identical content is auto-repaired by removing
the working-copy file so the symlink can be restored. Diverged content prompts
for which side wins.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRelink(newConsoleReporter())
		},
	}
}

func runRelink(r Reporter) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ws, err := project.Locate(cwd)
	if err != nil {
		return err
	}
	// Locate tolerates a malformed .mws.toml; surface the parse error here so
	// the user isn't told "nothing to relink" when the real problem is config.
	if _, err := config.Load(ws.MetaRoot); err != nil {
		return err
	}

	peers, err := ws.EnumerateCopies()
	if err != nil {
		return err
	}
	if len(peers) == 0 {
		r.Info("No working copies to relink.")
		return nil
	}

	for _, peer := range peers {
		r.Heading(fmt.Sprintf("Relinking %s ...", filepath.Base(peer)))
		if err := repairDivergedFiles(r, ws.MetaRoot, peer, huhDivergencePrompt); err != nil {
			r.Fail(fmt.Sprintf("%s: divergence repair: %v", filepath.Base(peer), err))
			continue
		}
		linked, err := project.LinkHarnessIntoWorkingCopy(ws.MetaRoot, peer)
		if err != nil {
			r.Fail(fmt.Sprintf("%s: %v", filepath.Base(peer), err))
			continue
		}
		if len(linked) == 0 {
			r.Info(fmt.Sprintf("%s: nothing to link", filepath.Base(peer)))
			continue
		}
		for _, name := range linked {
			r.OK(fmt.Sprintf("%s: linked %s", filepath.Base(peer), name))
		}
	}
	return nil
}

// divergencePrompt asks the user what to do when peer/<name> and meta/<name> are both regular
// files with different content. Returns one of: "peer", "meta", "skip".
type divergencePrompt func(peerName, fileName string) (string, error)

// repairDivergedFiles detects top-level entries where the peer has a regular file (not a
// symlink) at a name the harness (<metaRoot>/.mws/) also has as a regular file. Identical
// content is auto-resolved by removing the peer copy (LinkHarnessIntoWorkingCopy then
// recreates the symlink). Divergent content goes through prompt.
func repairDivergedFiles(r Reporter, metaRoot, peer string, prompt divergencePrompt) error {
	harnessRoot := filepath.Join(metaRoot, project.HarnessDirName)
	entries, err := os.ReadDir(peer)
	if err != nil {
		return err
	}
	for _, e := range entries {
		peerPath := filepath.Join(peer, e.Name())
		st, err := os.Lstat(peerPath)
		if err != nil || st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
			continue
		}
		metaPath := filepath.Join(harnessRoot, e.Name())
		mst, err := os.Lstat(metaPath)
		if err != nil || !mst.Mode().IsRegular() {
			continue
		}

		same, err := filesEqual(peerPath, metaPath)
		if err != nil {
			return err
		}
		if same {
			if err := os.Remove(peerPath); err != nil {
				return err
			}
			r.OK(fmt.Sprintf("%s: %s identical to meta, removed peer copy", filepath.Base(peer), e.Name()))
			continue
		}

		choice, err := prompt(filepath.Base(peer), e.Name())
		if err != nil {
			return err
		}
		switch choice {
		case "peer":
			if err := moveFile(peerPath, metaPath); err != nil {
				return fmt.Errorf("move %s -> %s: %w", peerPath, metaPath, err)
			}
			r.OK(fmt.Sprintf("%s: promoted peer %s into meta", filepath.Base(peer), e.Name()))
		case "meta":
			if err := os.Remove(peerPath); err != nil {
				return err
			}
			r.OK(fmt.Sprintf("%s: discarded peer %s, kept meta version", filepath.Base(peer), e.Name()))
		default:
			r.Warn(fmt.Sprintf("%s: %s left diverged", filepath.Base(peer), e.Name()))
		}
	}
	return nil
}

func huhDivergencePrompt(peerName, fileName string) (string, error) {
	var choice string
	err := huh.NewSelect[string]().
		Title(fmt.Sprintf("%s: %s differs from meta. Which to keep?", peerName, fileName)).
		Options(
			huh.NewOption("Peer (promote into meta, other peers will link to it)", "peer"),
			huh.NewOption("Meta (discard peer edits, restore symlink)", "meta"),
			huh.NewOption("Skip (leave diverged)", "skip"),
		).
		Value(&choice).
		Run()
	return choice, err
}

func filesEqual(a, b string) (bool, error) {
	af, err := os.Open(a)
	if err != nil {
		return false, err
	}
	defer af.Close()
	bf, err := os.Open(b)
	if err != nil {
		return false, err
	}
	defer bf.Close()

	const bufSize = 64 * 1024
	abuf := make([]byte, bufSize)
	bbuf := make([]byte, bufSize)
	for {
		an, aerr := io.ReadFull(af, abuf)
		bn, berr := io.ReadFull(bf, bbuf)
		if an != bn || !bytes.Equal(abuf[:an], bbuf[:bn]) {
			return false, nil
		}
		if aerr == io.EOF || aerr == io.ErrUnexpectedEOF {
			return berr == io.EOF || berr == io.ErrUnexpectedEOF, nil
		}
		if aerr != nil {
			return false, aerr
		}
	}
}
