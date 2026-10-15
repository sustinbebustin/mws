package worktree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sustinbebustin/mws/internal/atomicfile"
	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/project"
)

// carryRoot fans the harness out into the worktree root, then mirrors the
// copy root: regular files are copied, non-repo directories (except the
// worktrees dir) are linked. A copy-root entry shadows a harness entry of the
// same name, as it does in the copy; the anchor's checked-out .gitignore is
// never replaced. It returns the SHA-256 of each copied file by name.
func carryRoot(r Reporter, metaRoot, copyPath, target, worktreesDir string) (map[string]string, error) {
	copied := map[string]string{}
	return copied, mirrorRoot(r, metaRoot, copyPath, target, worktreesDir, copied)
}

// Relink refreshes the links of the worktree at root: harness entries and the
// copy-root directories it mirrors, with copy-root entries still shadowing
// harness ones. Files copied into the root at creation are left alone; they
// are the worktree's own from then on.
func Relink(metaRoot, copyPath, root string, policy config.WorktreePolicy) error {
	return mirrorRoot(silent{}, metaRoot, copyPath, root, policy.Dir, nil)
}

// silent discards progress lines.
type silent struct{}

func (silent) OK(string)   {}
func (silent) Warn(string) {}

// mirrorRoot is carryRoot, copying the copy root's regular files only when
// copied is non-nil (recording each copy's hash there), so a relink never
// overwrites the worktree's own copies.
func mirrorRoot(r Reporter, metaRoot, copyPath, target, worktreesDir string, copied map[string]string) error {
	if _, err := project.LinkHarnessIntoWorkingCopy(metaRoot, target); err != nil {
		return err
	}
	r.OK("Linked harness")
	entries, err := os.ReadDir(copyPath)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		src := filepath.Join(copyPath, name)
		dst := filepath.Join(target, name)
		if name == worktreesDir {
			continue
		}
		st, err := os.Lstat(src)
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() && !st.IsDir() {
			continue // harness symlinks were linked above
		}
		if st.IsDir() {
			if _, err := os.Lstat(filepath.Join(src, ".git")); err == nil {
				continue // native repo: has its own worktree
			}
		}
		if fi, err := os.Lstat(dst); err == nil && fi.Mode()&os.ModeSymlink == 0 {
			continue // the anchor's checkout or a native repo worktree
		}
		if st.IsDir() {
			rel, err := filepath.Rel(target, src)
			if err != nil {
				return err
			}
			if err := project.AtomicSymlink(rel, dst); err != nil {
				return err
			}
			r.OK(fmt.Sprintf("Linked %s", name))
			continue
		}
		if copied == nil {
			continue
		}
		if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := atomicfile.Copy(src, dst); err != nil {
			return err
		}
		if copied[name], err = fileSHA256(dst); err != nil {
			return err
		}
		r.OK(fmt.Sprintf("Copied %s", name))
	}
	return nil
}

// carryEnvs copies each configured env target from the copy's own repo, which
// already holds this copy's values, into the matching inner worktree.
func carryEnvs(r Reporter, cfg *config.Config, copyPath, target string, plan []plannedRepo) error {
	byFolder := map[string]config.Repo{}
	for _, repo := range append(append([]config.Repo{}, cfg.Repos...), cfg.OptionalRepos...) {
		byFolder[repo.Folder] = repo
	}
	for _, p := range plan {
		for _, env := range byFolder[p.folder].Envs {
			src := filepath.Join(copyPath, p.folder, env.Target)
			if _, err := os.Stat(src); err != nil {
				r.Warn(fmt.Sprintf("%s: env target %s is missing in the working copy, so the worktree has none; run `mws env sync` for the copy, then copy it in", p.folder, env.Target))
				continue
			}
			if err := atomicfile.Copy(src, filepath.Join(target, p.folder, env.Target)); err != nil {
				return fmt.Errorf("%s: %w", p.folder, err)
			}
			r.OK(fmt.Sprintf("%s: copied env %s", p.folder, env.Target))
		}
	}
	return nil
}
