package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/clierr"
	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/git"
	"github.com/sustinbebustin/mws/internal/ports"
	"github.com/sustinbebustin/mws/internal/project"
)

// setupChoice models the three policies for running [[repos.setup]] commands
// at the end of `mws clone`. Collapsing the (--setup, --no-setup) flag pair
// into a single enum at the command boundary keeps the rest of the file free
// of a two-bool state that has no meaningful "both true" representation.
type setupChoice int

const (
	setupAsk      setupChoice = iota // no flag -- prompt the user
	setupForceRun                    // --setup
	setupSkip                        // --no-setup
)

// optionalKind is how clone picks [[optional_repos]].
type optionalKind int

const (
	optionalAsk   optionalKind = iota // no flag -- prompt the user
	optionalNamed                     // --with
	optionalNone                      // --no-optional
)

// optionalChoice is clone's [[optional_repos]] selection; names is set only
// for optionalNamed.
type optionalChoice struct {
	kind  optionalKind
	names []string
}

// optionalChoiceFlags maps --with / --no-optional to an optionalChoice,
// rejecting both at once.
func optionalChoiceFlags(cmd *cobra.Command, with []string) (optionalChoice, error) {
	if err := exclusiveFlags(cmd, "with", "no-optional"); err != nil {
		return optionalChoice{}, err
	}
	switch {
	case cmd.Flags().Changed("no-optional"):
		return optionalChoice{kind: optionalNone}, nil
	case len(with) > 0:
		return optionalChoice{kind: optionalNamed, names: with}, nil
	}
	return optionalChoice{kind: optionalAsk}, nil
}

// setupChoiceFlags maps the --setup / --no-setup pair to a setupChoice,
// rejecting both at once.
func setupChoiceFlags(cmd *cobra.Command) (setupChoice, error) {
	if err := exclusiveFlags(cmd, "setup", "no-setup"); err != nil {
		return setupAsk, err
	}
	switch {
	case cmd.Flags().Changed("setup"):
		return setupForceRun, nil
	case cmd.Flags().Changed("no-setup"):
		return setupSkip, nil
	}
	return setupAsk, nil
}

func newCloneCmd() *cobra.Command {
	var with []string
	var claimPorts, jsonOut bool
	cmd := &cobra.Command{
		Use:   "clone <name>",
		Short: "Create a new working copy inside this meta workspace",
		Long: `clone creates a new working copy at <meta-root>/<name>/ and prints its path as
the last stdout line. It clones each registered native repo from its
configured URL into the new working copy, fans the harness symlinks out from
.mws/, and copies any env files mapped in .mws.toml from env staging into the
working copy.

Native repos are checked out on each repo's default branch -- a deliberate
fresh-start semantic.

When [[optional_repos]] are registered, clone offers them in a multiselect
prompt (default none). --with <folder> (repeatable) includes specific ones and
--no-optional includes none, both without prompting.

After clone and env-copy succeed, any [[repos.setup]] commands run inside
each cloned repo via sh -c, then the root [[setup]] commands at the copy
root. By default a single confirmation prompt summarises all configured
commands. Use --setup to run without prompting or --no-setup to skip
entirely. Without a terminal, every choice above must come from a flag; clone
fails before creating anything otherwise.

--ports claims the lowest free Port slot for the copy after its repos are
cloned, as ` + "`mws ports claim`" + ` does, so its env files are rendered and its
setup runs once, at the final ports. Without --ports a copy starts on the
default ports, and when .mws.toml declares [ports] clone ends with a warning
that it shares them.

Re-running for an existing working copy finishes it: registered repos (and
--with ones) that are missing are cloned and get their env files, setup runs
for those, and the path is printed. Repos already there are left as they
are; a re-run that clones nothing changes nothing and runs no setup, unless
--ports is given: then it claims a slot (a copy that holds one keeps it),
re-renders the copy and runs the root [[setup]] commands once.

--json prints {"copy", "path", "repos", "setup", "ports"} instead of the path:
the repos cloned this run, the setup outcome ("none" when no command was
scheduled, "skipped" for --no-setup or a declined prompt, "ran") and the
copy's ports ("default" or "slot N"). A failure prints
{"error": {"code", "message"}}.`,
		Example: `  mws clone feature-x
  mws clone feature-x --no-optional --no-setup
  mws clone feature-x --ports --no-optional --setup
  mws clone feature-x --with docs --with infra --setup
  mws clone feature-x --no-optional --setup --json
  cd "$(mws clone feature-x --no-optional --setup | tail -n1)"`,
		Args: usageCheckedArgs(cobra.MaximumNArgs(1), &jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			o := newOutput(cmd, jsonOut)
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			choice, err := setupChoiceFlags(cmd)
			if err != nil {
				return o.fail(err)
			}
			optional, err := optionalChoiceFlags(cmd, with)
			if err != nil {
				return o.fail(err)
			}
			return runClone(cmd.Context(), o.reporter(), o, name, choice, optional, claimPorts)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print the result as JSON")
	cmd.Flags().BoolVar(&claimPorts, "ports", false, "claim the lowest free Port slot for the new copy")
	cmd.Flags().Bool("setup", false, "run [[repos.setup]] and root [[setup]] commands without prompting")
	cmd.Flags().Bool("no-setup", false, "skip [[repos.setup]] and root [[setup]] commands without prompting")
	cmd.Flags().StringSliceVar(&with, "with", nil, "include named optional repo(s) in this working copy (repeatable)")
	cmd.Flags().Bool("no-optional", false, "include no optional repos, without prompting")
	return cmd
}

// setupOutcome says what clone did about its setup commands.
type setupOutcome string

const (
	setupNone    setupOutcome = "none"    // no setup command was scheduled
	setupSkipped setupOutcome = "skipped" // scheduled, but --no-setup or a declined prompt
	setupRan     setupOutcome = "ran"     // every scheduled command ran and passed
)

// cloneResult is the outcome of a clone that ended with the working copy in
// place: its path, the repos this run cloned (none for a copy that was
// already complete), what became of the setup commands, and the copy's ports
// ("default" or "slot N").
type cloneResult struct {
	Copy  string       `json:"copy"`
	Path  string       `json:"path"`
	Repos []string     `json:"repos"`
	Setup setupOutcome `json:"setup"`
	Ports string       `json:"ports"`
}

// runClone clones the working copy and reports it: its path as the last stdout
// line, or the cloneResult under --json. r is where progress goes.
func runClone(ctx context.Context, r Reporter, o output, name string, choice setupChoice, optional optionalChoice, claimPorts bool) error {
	ws, cfg, err := o.locate()
	if err != nil {
		return err
	}
	res, err := cloneCopy(ctx, r, ws, cfg, cloneRequest{name: name, setup: choice, optional: optional, ports: claimPorts})
	if err != nil {
		return o.fail(err)
	}
	writeCdFile(r, res.Path)
	return o.result(res, func(w io.Writer) error {
		_, err := fmt.Fprintln(w, res.Path)
		return err
	})
}

// cloneRequest is what one `mws clone` run was asked for.
type cloneRequest struct {
	name     string // empty: prompt for it
	setup    setupChoice
	optional optionalChoice
	ports    bool // --ports: claim a Port slot before the render
}

// folders lists the folder of each repo.
func folders(list []config.Repo) []string {
	out := make([]string, 0, len(list))
	for _, repo := range list {
		out = append(out, repo.Folder)
	}
	return out
}

// cloneCopy creates or finishes the working copy req names in ws. Every
// choice (name, optional repos, setup) is resolved before the first
// filesystem mutation, so an unknown --with name, a cancelled prompt or a
// missing flag fails without leaving a half-built copy.
func cloneCopy(ctx context.Context, r Reporter, ws *project.Workspace, cfg *config.Config, req cloneRequest) (cloneResult, error) {
	if err := project.ValidateWorkingCopiesDir(ws.WorkingCopiesDir); err != nil {
		return cloneResult{}, fmt.Errorf("invalid working_copies_dir %q in .mws.toml: %w", ws.WorkingCopiesDir, err)
	}
	name, err := cloneName(ws, req.name)
	if err != nil {
		return cloneResult{}, err
	}
	var policy config.PortPolicy
	if req.ports {
		if policy, err = portPolicy(cfg, ws); err != nil {
			return cloneResult{}, err
		}
	}
	target, existing, err := cloneTarget(ws, name)
	if err != nil {
		return cloneResult{}, err
	}
	repos, err := cloneRepos(cfg, target, existing, req.optional)
	if err != nil {
		return cloneResult{}, err
	}
	missing := missingRepos(target, repos)
	if existing && len(missing) == 0 {
		r.Info(fmt.Sprintf("Working copy %s already exists and has every repo", target))
		if !req.ports {
			warnDefaultPorts(r, cfg, target)
			return cloneResult{Copy: name, Path: target, Repos: []string{}, Setup: setupNone, Ports: portsLabel(r, target)}, nil
		}
	}
	// [[repos.setup]] runs only for the repos this run clones; the root
	// [[setup]] follows it, after the render.
	items := append(collectSetup(missing), rootSetupItems(cfg)...)
	if err := requireSetupChoice(req.setup, items, "mws clone "+name); err != nil {
		return cloneResult{}, err
	}

	failed, err := populateCopy(ctx, r, ws.MetaRoot, target, missing)
	if err != nil {
		return cloneResult{}, err
	}
	// Claim before the render, so the env files and the setup below see the
	// final ports.
	var claimErr error
	if req.ports {
		var slot, previous int
		if slot, previous, claimErr = claimLease(r, ws, cfg, policy, target, nil, false); claimErr == nil {
			r.OK(fmt.Sprintf("%s: %s -> %s", name, slotLabel(previous), slotLabel(slot)))
		}
	}
	renderEnv(r, ws.MetaRoot, cfg, target, false)

	if len(failed) > 0 {
		return cloneResult{}, fmt.Errorf("clone completed with errors: %d repo(s) failed: %s; the rest are in place, so rerunning the same command clones only these",
			len(failed), strings.Join(failed, ", "))
	}
	if claimErr != nil {
		return cloneResult{}, fmt.Errorf("clone completed but claimed no Port slot: %w; the copy is in place on the default ports and no setup command ran. Claim one with `mws ports claim %s`, then run the setup commands by hand",
			claimErr, name)
	}
	setup, err := cloneSetup(ctx, r, ws.MetaRoot, cfg, target, req.setup, items)
	if err != nil {
		return cloneResult{}, err
	}

	r.OK(fmt.Sprintf("Working copy ready at %s", target))
	warnDefaultPorts(r, cfg, target)
	return cloneResult{Copy: name, Path: target, Repos: folders(missing), Setup: setup, Ports: portsLabel(r, target)}, nil
}

// cloneName is the validated name of the copy to clone: name, or when it is
// empty, the answer to a prompt.
func cloneName(ws *project.Workspace, name string) (string, error) {
	if name == "" {
		if !canPrompt() {
			return "", noPrompt("no working copy name given", "mws clone <name> --no-optional --setup", "mws clone <name> --no-optional --no-setup")
		}
		if err := runField(huh.NewInput().
			Title("New working copy name").
			Description(clonePromptDescription(ws)).
			Validate(project.ValidateName).
			Value(&name)); err != nil {
			return "", err
		}
		name = strings.TrimSpace(name)
	}
	if err := project.ValidateName(name); err != nil {
		return "", usagef("working copy name %q: %w", name, err)
	}
	return name, nil
}

// cloneTarget is where the copy called name lives, and whether a working
// copy is already there. Anything else at that path is refused.
func cloneTarget(ws *project.Workspace, name string) (target string, existing bool, err error) {
	target = filepath.Join(ws.CopiesRoot(), name)
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		return target, false, nil
	} else if err != nil {
		return "", false, err
	}
	if !looksLikeWorkingCopy(ws.MetaRoot, target) {
		return "", false, clierr.Refusedf("%s already exists and is not a working copy of %s; nothing was changed; choose another name", target, ws.MetaRoot)
	}
	return target, true, nil
}

// cloneRepos is every repo the copy at target should hold: the default repos
// and the chosen optional ones. A retry without --with or --no-optional keeps
// the optional repos the copy already has; it does not ask again.
func cloneRepos(cfg *config.Config, target string, existing bool, optional optionalChoice) ([]config.Repo, error) {
	var selected []config.Repo
	if existing && optional.kind == optionalAsk {
		selected = presentRepos(target, cfg.OptionalRepos)
	} else {
		var err error
		if selected, err = resolveOptional(cfg, optional); err != nil {
			return nil, err
		}
	}
	return slices.Concat(cfg.Repos, selected), nil
}

// populateCopy creates the copy at target, links the harness into it and
// clones each of missing. A repo that fails to clone is reported on r and
// named in failed; the rest are still cloned.
func populateCopy(ctx context.Context, r Reporter, metaRoot, target string, missing []config.Repo) (failed []string, err error) {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return nil, err
	}
	linked, err := project.LinkHarnessIntoWorkingCopy(metaRoot, target)
	if err != nil {
		return nil, err
	}
	for _, n := range linked {
		r.OK(fmt.Sprintf("Linked %s/%s", filepath.Base(target), n))
	}
	for _, repo := range missing {
		if err := cloneNative(ctx, r, repo, target); err != nil {
			r.Fail(fmt.Sprintf("%s: %v", repo.Folder, err))
			failed = append(failed, repo.Folder)
		}
	}
	return failed, nil
}

// cloneSetup runs the setup items of a clone into the copy at target, when
// choice (or the prompt) says so, and says what became of them.
func cloneSetup(ctx context.Context, r Reporter, metaRoot string, cfg *config.Config, target string, choice setupChoice, items []setupItem) (setupOutcome, error) {
	if len(items) == 0 {
		return setupNone, nil
	}
	run, err := confirmSetup(choice, items)
	if err != nil || !run {
		return setupSkipped, err
	}
	exports, err := copyExports(metaRoot, cfg, target)
	if err != nil {
		return "", fmt.Errorf("clone completed but no setup command ran: %w", err)
	}
	if failed := runSetup(ctx, r, target, exports, items, os.Stderr, os.Stderr); len(failed) > 0 {
		return "", fmt.Errorf("clone completed but %d setup command(s) failed:\n  %s\nthe working copy is kept; fix the cause, then run the failed command(s) by hand",
			len(failed), strings.Join(failed, "\n  "))
	}
	return setupRan, nil
}

// warnDefaultPorts warns that the working copy at target shares the default
// ports with every other copy without a Port slot, when .mws.toml declares
// [ports] and the copy holds no slot.
func warnDefaultPorts(r Reporter, cfg *config.Config, target string) {
	if _, ok := cfg.PortPolicy(); !ok {
		return
	}
	if slot, err := ports.ReadSlot(target); err != nil || slot != 0 {
		return
	}
	name := filepath.Base(target)
	r.Warn(fmt.Sprintf("%s is on the default ports, which it shares with every copy without a Port slot; give it its own with `mws ports claim %s`", name, name))
}

// missingRepos returns the repos of list with no directory in the copy at
// target.
func missingRepos(target string, list []config.Repo) []config.Repo {
	var out []config.Repo
	for _, repo := range list {
		if _, err := os.Stat(filepath.Join(target, repo.Folder)); err != nil {
			out = append(out, repo)
		}
	}
	return out
}

// presentRepos returns the repos of list that the copy at target holds.
func presentRepos(target string, list []config.Repo) []config.Repo {
	var out []config.Repo
	for _, repo := range list {
		if _, err := os.Stat(filepath.Join(target, repo.Folder)); err == nil {
			out = append(out, repo)
		}
	}
	return out
}

// requireSetupChoice fails before anything changes when setup commands would
// need the confirmation prompt and there is no terminal. invocation is the
// command line to repeat with --setup or --no-setup.
func requireSetupChoice(choice setupChoice, items []setupItem, invocation string) error {
	if len(items) == 0 || choice != setupAsk || canPrompt() {
		return nil
	}
	return noPrompt("setup commands are configured and neither --setup nor --no-setup was given",
		invocation+" --setup", invocation+" --no-setup")
}

// writeCdFile writes target to $MWS_CD_FILE when the user's mws shell function
// set it, so the parent shell can cd into the copy -- a child process can't
// chdir its parent (ADR-0008). Failing to write only costs the auto-cd, so it
// warns.
func writeCdFile(r Reporter, target string) {
	if cdFile := os.Getenv("MWS_CD_FILE"); cdFile != "" {
		if err := os.WriteFile(cdFile, []byte(target), 0o600); err != nil {
			r.Warn(fmt.Sprintf("could not write MWS_CD_FILE %s: %v", cdFile, err))
		}
	}
}

// clonePromptDescription renders the help text shown under the "New working
// copy name" prompt, reflecting the configured working_copies_dir (if any).
func clonePromptDescription(ws *project.Workspace) string {
	if ws.WorkingCopiesDir != "" {
		return fmt.Sprintf("Will be created at <meta-root>/%s/<name>/.", ws.WorkingCopiesDir)
	}
	return "Will be created at <meta-root>/<name>/."
}

// resolveOptional decides which [[optional_repos]] to include in this clone.
// With no optional repos registered it returns nil without prompting. When
// --with names are supplied they select non-interactively: each must match a
// registered optional folder or the clone fails before any optional repo is
// cloned. --no-optional selects none. Otherwise the user is offered a
// multiselect defaulting to none.
func resolveOptional(cfg *config.Config, choice optionalChoice) ([]config.Repo, error) {
	switch choice.kind {
	case optionalNone:
		return nil, nil
	case optionalNamed:
		out := make([]config.Repo, 0, len(choice.names))
		for _, name := range choice.names {
			folder := strings.TrimSpace(name)
			repo, ok := cfg.OptionalRepo(folder)
			if !ok {
				return nil, usagef("--with %q: no optional repo registered with that folder (registered: %s)", folder, listOptionalFolders(cfg))
			}
			out = append(out, repo)
		}
		return out, nil
	}
	if len(cfg.OptionalRepos) == 0 {
		return nil, nil
	}
	if !canPrompt() {
		return nil, noPrompt(fmt.Sprintf("optional repos are registered (%s) and neither --with nor --no-optional was given", listOptionalFolders(cfg)),
			"mws clone <name> --no-optional", "mws clone <name> --with <folder>")
	}
	opts := make([]huh.Option[string], 0, len(cfg.OptionalRepos))
	for _, repo := range cfg.OptionalRepos {
		opts = append(opts, huh.NewOption(repo.Folder, repo.Folder))
	}
	var picked []string
	if err := runField(huh.NewMultiSelect[string]().
		Title("Include optional repos?").
		Description("Space to select, enter to confirm. Select none to skip.").
		Options(opts...).
		Value(&picked)); err != nil {
		return nil, err
	}
	out := make([]config.Repo, 0, len(picked))
	for _, folder := range picked {
		if repo, ok := cfg.OptionalRepo(folder); ok {
			out = append(out, repo)
		}
	}
	return out, nil
}

// listOptionalFolders renders the registered optional-repo folders as a
// comma-separated list, or "(none)" when there are none. Used in error and
// guidance messages for `mws clone --with` and `mws include`.
func listOptionalFolders(cfg *config.Config) string {
	if len(cfg.OptionalRepos) == 0 {
		return "(none)"
	}
	folders := make([]string, 0, len(cfg.OptionalRepos))
	for _, repo := range cfg.OptionalRepos {
		folders = append(folders, repo.Folder)
	}
	return strings.Join(folders, ", ")
}

// cloneNative populates target/<folder> with a fresh `git clone <url>` of the
// repo and then checks out its default branch. The URL is required: cloning
// from a sibling working copy is intentionally not supported, so the new copy
// never inherits divergent `.git/` state from a donor.
func cloneNative(ctx context.Context, r Reporter, repo config.Repo, target string) error {
	dst := filepath.Join(target, repo.Folder)
	if _, err := os.Stat(dst); err == nil {
		r.Info(fmt.Sprintf("%s: already present, skipping", repo.Folder))
		return nil
	}
	if repo.URL == "" {
		return fmt.Errorf("no remote URL configured in .mws.toml")
	}
	r.Heading(fmt.Sprintf("Cloning %s ...", repo.Folder))
	start := time.Now()
	if err := git.Clone(ctx, repo.URL, dst); err != nil {
		return err
	}
	r.Done(fmt.Sprintf("%s: cloned", repo.Folder), time.Since(start))
	return checkoutDefault(ctx, r, dst)
}

func checkoutDefault(ctx context.Context, r Reporter, repoDir string) error {
	br, err := git.DefaultBranch(ctx, repoDir, "origin")
	if err != nil {
		r.Warn(fmt.Sprintf("%s: could not determine default branch (%v)", filepath.Base(repoDir), err))
		return nil
	}
	if cur, err := git.CurrentBranch(ctx, repoDir); err == nil && cur == br {
		r.OK(fmt.Sprintf("%s: on default branch %s", filepath.Base(repoDir), br))
		return nil
	}
	if err := git.Checkout(ctx, repoDir, br); err != nil {
		r.Warn(fmt.Sprintf("%s: could not checkout %s (%v)", filepath.Base(repoDir), br, err))
		return nil
	}
	r.OK(fmt.Sprintf("%s: checked out default branch %s", filepath.Base(repoDir), br))
	return nil
}

// confirmSetup decides whether [[repos.setup]] commands run. With explicit
// flags the answer is immediate; otherwise the user is prompted with a
// grouped listing of every configured command. An empty items slice always
// returns (false, nil) without prompting -- nothing to ask about.
func confirmSetup(choice setupChoice, items []setupItem) (bool, error) {
	if len(items) == 0 {
		return false, nil
	}
	switch choice {
	case setupForceRun:
		return true, nil
	case setupSkip:
		return false, nil
	}
	// Callers check requireSetupChoice before changing anything, so a
	// terminal is there to ask on.
	var ok bool
	if err := runField(huh.NewConfirm().
		Title("Run setup commands?").
		Description(setupPromptBody(items)).
		Affirmative("Run").
		Negative("Skip").
		Value(&ok)); err != nil {
		return false, err
	}
	return ok, nil
}

// setupPromptBody renders items grouped by repo folder, two-space indented
// under each folder heading. See docs/adr/0006-post-clone-setup-commands.md.
func setupPromptBody(items []setupItem) string {
	var b strings.Builder
	var lastFolder string
	for i, it := range items {
		if it.Folder != lastFolder {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(it.Folder)
			b.WriteString("\n")
			lastFolder = it.Folder
		}
		b.WriteString("  ")
		b.WriteString(it.Cmd)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// setupItem is one flat (repo folder, shell command) pair scheduled by
// collectSetup. The flat shape lets confirmSetup/runSetup iterate without
// re-walking the config and decouples them from the config types.
type setupItem struct {
	Folder string
	Cmd    string
}

// collectSetup returns the [[repos.setup]] commands across repos as a flat,
// ordered slice. Each cmd is trimmed; entries that are empty after trimming
// are dropped. Repos with no setup contribute nothing. An empty return means
// callers should short-circuit without prompting or executing. Callers pass
// the repos to schedule (default repos, optionally plus included optional
// repos), so the same routine serves both `mws clone` and `mws include`.
func collectSetup(repos []config.Repo) []setupItem {
	var out []setupItem
	for _, repo := range repos {
		for _, sc := range repo.Setup {
			cmd := strings.TrimSpace(sc.Cmd)
			if cmd == "" {
				continue
			}
			out = append(out, setupItem{Folder: repo.Folder, Cmd: cmd})
		}
	}
	return out
}

// runSetup executes items in order against target/<folder>, with the working
// copy's export set added to the environment. Commands inside a single repo
// run sequentially and stop at the first non-zero exit; failures in one repo
// do not block other repos. Returns "<folder>: <cmd>" strings for each
// failure -- nil if everything passed. stdout/stderr are injected so
// production can stream live to the terminal and tests can discard.
func runSetup(ctx context.Context, r Reporter, target string, exports ports.Exports, items []setupItem, stdout, stderr io.Writer) []string {
	var failed []string
	var skipFolder, lastHeading string
	for _, it := range items {
		if it.Folder == skipFolder {
			continue
		}
		if it.Folder != lastHeading {
			r.Heading(fmt.Sprintf("Setup: %s ...", it.Folder))
			lastHeading = it.Folder
		}
		r.Info(fmt.Sprintf("$ %s", it.Cmd))
		start := time.Now()

		cmd := exec.CommandContext(ctx, "sh", "-c", it.Cmd)
		cmd.Dir = filepath.Join(target, it.Folder)
		cmd.Env = append(os.Environ(), exports.Environ()...)
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		cmd.Stdin = nil

		if err := cmd.Run(); err != nil {
			r.Fail(fmt.Sprintf("%s: %s (%v)", it.Folder, it.Cmd, err))
			failed = append(failed, fmt.Sprintf("%s: %s", it.Folder, it.Cmd))
			skipFolder = it.Folder
			continue
		}
		r.Done(fmt.Sprintf("%s: %s", it.Folder, it.Cmd), time.Since(start))
	}
	return failed
}
