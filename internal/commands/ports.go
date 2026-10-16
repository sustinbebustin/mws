package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sustinbebustin/mws/internal/clierr"
	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/ports"
	"github.com/sustinbebustin/mws/internal/project"
)

const portsLong = `ports manages the Port slot of each working copy. A copy starts on the
default ports (slot 0: every [[ports.port]] at its base). Claiming slot k >= 1
moves every port to base + k * stride; the slot is recorded in the copy's Port
lease, <copy>/.mws-ports.env, which also exports every port, builtin and
[vars] entry. Claiming and releasing re-render the copy's env files and
[[renders]] targets at the new slot, refresh its worktrees' carried files and
run the root [[setup]] commands.

Each subcommand defaults to the working copy containing the current
directory; run inside a worktree, it acts on the worktree's working copy.
Every subcommand is non-interactive. Results go to stdout, progress to stderr.
--json prints one JSON object, or {"error": {"code", "message"}} on failure.

Exit codes: 0 ok, 1 failure, 2 usage, 3 refused (a slot another copy holds,
or conflicting leases), 4 no working copy by that name.`

func newPortsCmd() *cobra.Command {
	var jsonOut, export bool
	cmd := &cobra.Command{
		Use:   "ports [copy]",
		Short: "Claim, release, show and check the Port slot of a working copy",
		Long: portsLong + "\n\nWithout a subcommand, ports runs show. `mws ports <arg>` with an arg that is\n" +
			"neither a subcommand nor a working copy is a usage error (exit 2), so a\n" +
			"mistyped subcommand is not taken for a copy; `mws ports show <copy>` exits 4.",
		Example: `  mws ports
  eval "$(mws ports show --export)"
  mws ports claim feature-x --no-setup
  mws ports ls --json`,
		Args: usageCheckedArgs(cobra.MatchAll(cobra.MaximumNArgs(1), copyOrSubcommand), &jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPortsShow(newOutput(cmd, jsonOut), copyArg(args), export)
		},
	}
	cmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "print results as JSON")
	addExportFlag(cmd, &export)
	cmd.AddCommand(
		newPortsShowCmd(&jsonOut),
		newPortsLsCmd(&jsonOut),
		newPortsClaimCmd(&jsonOut),
		newPortsReleaseCmd(&jsonOut),
		newPortsCheckCmd(&jsonOut),
	)
	return cmd
}

// copyOrSubcommand rejects the argument of a bare `mws ports <arg>` that names
// no working copy as a usage error, so a mistyped subcommand ("chek") exits 2
// instead of 4. `mws ports show <copy>` still reports an unknown copy as not
// found. A workspace that cannot be located passes, for RunE to report.
func copyOrSubcommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	ws, err := project.Locate(cwd)
	if err != nil {
		return nil
	}
	if _, err := ws.ResolveCopy(args[0]); !errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	var subs []string
	for _, c := range cmd.Commands() {
		if c.IsAvailableCommand() {
			subs = append(subs, c.Name())
		}
	}
	return usagef("%q is neither a subcommand of `%s` (%s) nor a working copy under %s; see `%s --help`",
		args[0], cmd.CommandPath(), strings.Join(subs, ", "), ws.CopiesRoot(), cmd.CommandPath())
}

func newPortsCheckCmd(jsonOut *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "check [copy]",
		Short: "Report which of a working copy's ports have a listener",
		Long: `check bind-probes every port of the working copy's slot on 127.0.0.1 and the
wildcard address and prints one "<name>  <port>  listening|free" line each. A
listener may be the copy's own stack or another process. It changes nothing
and exits 0 either way; a port mws cannot probe (a privileged one, say) is a
failure.

--json prints {"copy", "path", "slot", "ports": [{"name", "port", "listening"}]}.`,
		Example: `  mws ports check
  mws ports check feature-x --json`,
		Args: usageCheckedArgs(cobra.MaximumNArgs(1), jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPortsCheck(newOutput(cmd, *jsonOut), copyArg(args))
		},
	}
}

type portCheckJSON struct {
	Name      string `json:"name"`
	Port      int    `json:"port"`
	Listening bool   `json:"listening"`
}

func runPortsCheck(o output, name string) error {
	ws, c, err := portsTarget(o, name, "mws ports check <copy>")
	if err != nil {
		return err
	}
	policy, err := portPolicy(c.cfg, ws)
	if err != nil {
		return o.fail(err)
	}
	slot, err := ports.ReadSlot(c.path)
	if err != nil {
		return o.fail(clierr.Failuref("%w", err))
	}
	slotPorts := ports.SlotPorts(policy, slot)
	listening, err := listeners(slotPorts)
	if err != nil {
		return o.fail(clierr.Failuref("%w", err))
	}
	checks := make([]portCheckJSON, len(slotPorts))
	width := 0
	for i, p := range policy.Ports {
		checks[i] = portCheckJSON{Name: p.Name, Port: slotPorts[i], Listening: slices.Contains(listening, slotPorts[i])}
		width = max(width, len(p.Name))
	}
	return o.result(portsCheck{Copy: c.name(), Path: c.path, Slot: slot, Ports: checks}, func(w io.Writer) error {
		for _, pc := range checks {
			state := "free"
			if pc.Listening {
				state = "listening"
			}
			if _, err := fmt.Fprintf(w, "%-*s  %d  %s\n", width, pc.Name, pc.Port, state); err != nil {
				return err
			}
		}
		return nil
	})
}

func newPortsLsCmd(jsonOut *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List every working copy's Port slot and the next free slot",
		Long: `ls prints one line per live working copy: "default" for a copy on the
default ports, "slot N" for one holding a lease, flagged "conflict: <copies>"
when two or more leases name the same slot, and a copy whose lease mws cannot
read as "invalid lease". The last line names the next free slot, the one a
claim without --slot would take if no listener holds its ports.

--json prints {"copies": [{"copy", "path", "slot", "conflict", "error"}],
"conflicts": [{"slot", "copies"}], "next_free"}, with next_free null when
.mws.toml has no [ports] table or every slot is held.`,
		Example: `  mws ports ls
  mws ports ls --json`,
		Args: usageCheckedArgs(cobra.NoArgs, jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPortsLs(newOutput(cmd, *jsonOut))
		},
	}
}

// portsLsCopy is one working copy in `ports ls`. Error is set, and Slot is
// 0, when its lease is unreadable.
type portsLsCopy struct {
	Copy     string `json:"copy"`
	Path     string `json:"path"`
	Slot     int    `json:"slot"`
	Conflict bool   `json:"conflict"`
	Error    string `json:"error,omitempty"`
}

type portsConflictJSON struct {
	Slot   int      `json:"slot"`
	Copies []string `json:"copies"`
}

func runPortsLs(o output) error {
	ws, cfg, err := o.locate()
	if err != nil {
		return err
	}
	copies, err := ws.EnumerateCopies()
	if err != nil {
		return o.fail(clierr.Failuref("list the working copies of %s: %w", ws.MetaRoot, err))
	}
	r := o.reporter()
	rows := make([]portsLsCopy, 0, len(copies))
	held := make([]ports.Holding, 0, len(copies))
	for _, c := range copies {
		row := portsLsCopy{Copy: filepath.Base(c), Path: c}
		slot, err := ports.ReadSlot(c)
		if err != nil {
			row.Error = err.Error()
			r.Warn(err.Error())
		} else {
			row.Slot = slot
			held = append(held, ports.Holding{Copy: row.Copy, Slot: slot})
		}
		rows = append(rows, row)
	}
	conflicts := ports.Conflicts(held)
	conflictsJSON := make([]portsConflictJSON, 0, len(conflicts))
	inConflict := map[string][]string{}
	for _, c := range conflicts {
		conflictsJSON = append(conflictsJSON, portsConflictJSON{Slot: c.Slot, Copies: c.Copies})
		for _, name := range c.Copies {
			inConflict[name] = c.Copies
		}
	}
	for i := range rows {
		_, rows[i].Conflict = inConflict[rows[i].Copy]
	}
	var nextFree *int
	nextNote := "none: .mws.toml has no [ports] table"
	if policy, ok := cfg.PortPolicy(); ok {
		if slot, err := ports.NextFree(held, policy.Limit, nil); err == nil {
			nextFree = &slot
		} else {
			nextNote = fmt.Sprintf("none: all %d slots are held", policy.Limit)
		}
	}
	return o.result(portsLs{Copies: rows, Conflicts: conflictsJSON, NextFree: nextFree}, func(w io.Writer) error {
		width := 0
		for _, row := range rows {
			width = max(width, len(row.Copy))
		}
		for _, row := range rows {
			state := slotLabel(row.Slot)
			switch {
			case row.Error != "":
				state = invalidLease
			case row.Conflict:
				state += "  conflict: " + strings.Join(inConflict[row.Copy], ", ")
			}
			if _, err := fmt.Fprintf(w, "%-*s  %s\n", width, row.Copy, state); err != nil {
				return err
			}
		}
		if nextFree != nil {
			_, err := fmt.Fprintf(w, "next free slot: %d\n", *nextFree)
			return err
		}
		_, err := fmt.Fprintf(w, "next free slot: %s\n", nextNote)
		return err
	})
}

func addExportFlag(cmd *cobra.Command, export *bool) {
	cmd.Flags().BoolVar(export, "export", false, `prefix each line with "export " for eval in a shell`)
}

func newPortsShowCmd(jsonOut *bool) *cobra.Command {
	var export bool
	cmd := &cobra.Command{
		Use:   "show [copy]",
		Short: "Print a working copy's slot and exported variables",
		Long: `show prints the working copy's export set, the variables its Port lease
exports, as one VAR=value line each: MWS_SLOT (0 on the default ports),
MWS_COPY, MWS_COPY_SLUG, MWS_META, then every port as MWS_PORT_<NAME> and its
alias, then the [vars] entries. A copy on the default ports has no lease and
shows the slot-0 values.

--export prefixes each line with "export ", so a shell without direnv loads
them with eval "$(mws ports show --export)". --json prints
{"copy", "path", "slot", "vars": {NAME: value}}.`,
		Example: `  mws ports show
  eval "$(mws ports show --export)"
  mws ports show feature-x --json`,
		Args: usageCheckedArgs(cobra.MaximumNArgs(1), jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPortsShow(newOutput(cmd, *jsonOut), copyArg(args), export)
		},
	}
	addExportFlag(cmd, &export)
	return cmd
}

func runPortsShow(o output, name string, export bool) error {
	_, c, err := portsTarget(o, name, "mws ports show <copy>")
	if err != nil {
		return err
	}
	exports, err := c.exports()
	if err != nil {
		return o.fail(clierr.Failuref("%w", err))
	}
	vars := make(map[string]string, len(exports.Vars))
	for _, v := range exports.Vars {
		vars[v.Name] = v.Value
	}
	return o.result(portsShow{Copy: c.name(), Path: c.path, Slot: exports.Slot, Vars: vars}, func(w io.Writer) error {
		prefix := ""
		if export {
			prefix = "export "
		}
		for _, line := range exports.Dotenv() {
			if _, err := fmt.Fprintln(w, prefix+line); err != nil {
				return err
			}
		}
		return nil
	})
}

func newPortsClaimCmd(jsonOut *bool) *cobra.Command {
	var dryRun bool
	var slot int
	cmd := &cobra.Command{
		Use:   "claim [copy]",
		Short: "Give a working copy its own Port slot and re-render it",
		Long: `claim gives the working copy the lowest free Port slot from 1, skipping a
slot whose ports have a listener right now (with a warning), writes its lease
under a lock on the meta root, and re-renders the copy at that slot: its env
files, its [[renders]] targets and its worktrees' carried files, then the root
[[setup]] commands. A copy that already holds a slot keeps it and is
re-rendered, so a retry is safe.

--slot N takes or moves to slot N without probing for listeners. claim refuses
(exit 3) a slot another live copy holds, and any claim without --slot while
two copies' leases name one slot; --slot settles such a conflict by moving one
of them.

--setup runs the root [[setup]] commands without prompting and --no-setup
skips them; without a terminal one of the two is required when [[setup]] is
configured. --dry-run prints "would claim slot N for <copy>" and one
"would copy <source> -> <target>" line per file, and changes nothing.

Prints "<copy> slot N" on stdout. --json prints
{"dry_run", "copy", "path", "slot", "previous_slot", "files": [...], "error"}
with files as in ` + "`mws env sync --json`" + `.`,
		Example: `  mws ports claim
  mws ports claim feature-x --slot 3 --no-setup
  mws ports claim --dry-run --json`,
		Args: usageCheckedArgs(cobra.MaximumNArgs(1), jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			o := newOutput(cmd, *jsonOut)
			choice, err := setupChoiceFlags(cmd)
			if err != nil {
				return o.fail(err)
			}
			var want *int
			if cmd.Flags().Changed("slot") {
				want = &slot
			}
			return runPortsClaim(cmd.Context(), o, copyArg(args), want, leaseRequest{dryRun: dryRun, setup: choice})
		},
	}
	cmd.Flags().IntVar(&slot, "slot", 0, "take or move to this slot, without probing for listeners")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "name the slot and files; change nothing")
	addSetupFlags(cmd)
	return cmd
}

func newPortsReleaseCmd(jsonOut *bool) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "release [copy]",
		Short: "Put a working copy back on the default ports and re-render it",
		Long: `release drops the working copy's Port lease and re-renders the copy at the
default ports (slot 0), as claim does at a slot, then runs the root [[setup]]
commands. The rendered files are then byte for byte those of a fresh clone. A
copy already on the default ports is left alone and release exits 0.

--setup, --no-setup and --dry-run work as for claim; --dry-run prints
"would release slot N from <copy>" and the "would copy" lines.

Prints "<copy> default" on stdout. --json prints the same object as claim,
with slot 0.`,
		Example: `  mws ports release
  mws ports release feature-x --no-setup`,
		Args: usageCheckedArgs(cobra.MaximumNArgs(1), jsonOut),
		RunE: func(cmd *cobra.Command, args []string) error {
			o := newOutput(cmd, *jsonOut)
			choice, err := setupChoiceFlags(cmd)
			if err != nil {
				return o.fail(err)
			}
			return runPortsRelease(cmd.Context(), o, copyArg(args), leaseRequest{dryRun: dryRun, setup: choice})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "name the slot and files; change nothing")
	addSetupFlags(cmd)
	return cmd
}

// portsCopy resolves the [copy] argument of a ports subcommand: a name with
// no working copy is NotFound (exit 4).
func portsCopy(ws *project.Workspace, name, invocation string) (string, error) {
	p, err := resolveCopy(ws, name, invocation)
	switch {
	case err == nil:
		return p, nil
	case errors.Is(err, fs.ErrNotExist):
		return "", clierr.NotFoundf("no working copy %q under %s; `mws ls` lists the copies", name, ws.CopiesRoot())
	case errors.As(err, new(*clierr.Error)):
		return "", err
	default:
		return "", clierr.Failuref("%w", err)
	}
}

// portPolicy is the workspace's port policy, or a usage error when
// .mws.toml declares no [ports] table.
func portPolicy(cfg *config.Config, ws *project.Workspace) (config.PortPolicy, error) {
	policy, ok := cfg.PortPolicy()
	if !ok {
		return config.PortPolicy{}, usagef("%s declares no [ports] table, so there are no Port slots; add [ports] with at least one [[ports.port]] (see docs/config.md)",
			filepath.Join(ws.MetaRoot, config.ConfigFileName))
	}
	return policy, nil
}

// portsChange is the result of `ports claim` and `ports release`: the slot
// the copy is on afterwards and the one it held before (0 for the default
// ports), with the render's files.
type portsChange struct {
	DryRun       bool            `json:"dry_run"`
	Copy         string          `json:"copy"`
	Path         string          `json:"path"`
	Slot         int             `json:"slot"`
	PreviousSlot int             `json:"previous_slot"`
	Files        []envFileResult `json:"files"`
	Error        string          `json:"error,omitempty"`
}

// portsShow is the result of `ports show`: the copy's slot and its export
// set by name.
type portsShow struct {
	Copy string            `json:"copy"`
	Path string            `json:"path"`
	Slot int               `json:"slot"`
	Vars map[string]string `json:"vars"`
}

// portsCheck is the result of `ports check`: each port of the copy's slot and
// whether a listener holds it.
type portsCheck struct {
	Copy  string          `json:"copy"`
	Path  string          `json:"path"`
	Slot  int             `json:"slot"`
	Ports []portCheckJSON `json:"ports"`
}

// portsLs is the result of `ports ls`: every live copy's slot, the slots two
// or more leases name, and the slot a claim would take (nil when there is
// none).
type portsLs struct {
	Copies    []portsLsCopy       `json:"copies"`
	Conflicts []portsConflictJSON `json:"conflicts"`
	NextFree  *int                `json:"next_free"`
}

// slotLabel names a slot as `ports ls` shows it: "default" or "slot N".
func slotLabel(slot int) string {
	if slot == 0 {
		return "default"
	}
	return "slot " + strconv.Itoa(slot)
}

// leaseRequest is the part of a `ports claim` or `ports release` run that
// both share: whether it only previews, the setup choice, and the command
// line that repeats it.
type leaseRequest struct {
	dryRun     bool
	setup      setupChoice
	invocation string
}

// leaseChange is what claim or release did, or under --dry-run would do, to a
// copy's Port lease: the slot it held before and the one it is on after (0
// for the default ports), with the change's "would" line.
type leaseChange struct {
	previous, slot int
	plan           string
}

// portsTarget locates the workspace and resolves the [copy] argument of a
// ports subcommand; usage names the command line with a <copy> placeholder.
// Errors have been reported through o.
func portsTarget(o output, name, usage string) (*project.Workspace, workingCopy, error) {
	ws, cfg, err := o.locate()
	if err != nil {
		return nil, workingCopy{}, err
	}
	copyPath, err := portsCopy(ws, name, usage)
	if err != nil {
		return nil, workingCopy{}, o.fail(err)
	}
	return ws, workingCopy{metaRoot: ws.MetaRoot, cfg: cfg, path: copyPath}, nil
}

// changeLease is the pipeline claim and release share once the copy c is
// known: decide the render mode (prompting for the root setup), let apply
// change the lease (or, previewing, only decide the change), then re-render
// the copy and print the result.
func changeLease(ctx context.Context, o output, c workingCopy, req leaseRequest, apply func(r Reporter, preview bool) (leaseChange, error)) error {
	mode, err := resolveRenderMode(c.cfg, req.dryRun, req.setup, req.invocation)
	if err != nil {
		return o.fail(err)
	}
	r := o.reporter()
	change, err := apply(r, mode == renderPreview)
	if err != nil {
		return o.fail(err)
	}
	if mode != renderPreview {
		r.OK(fmt.Sprintf("%s: %s -> %s", c.name(), slotLabel(change.previous), slotLabel(change.slot)))
	}
	return finishPortsChange(ctx, o, r, c, mode, change, req.invocation)
}

func runPortsClaim(ctx context.Context, o output, name string, want *int, req leaseRequest) error {
	ws, c, err := portsTarget(o, name, "mws ports claim <copy>")
	if err != nil {
		return err
	}
	// Without [ports] there is nothing to claim: fail before the setup prompt.
	if _, err := portPolicy(c.cfg, ws); err != nil {
		return o.fail(err)
	}
	req.invocation = "mws ports claim " + c.name()
	if want != nil {
		req.invocation += " --slot " + strconv.Itoa(*want)
	}
	return changeLease(ctx, o, c, req, func(r Reporter, preview bool) (leaseChange, error) {
		return claimLease(r, ws, c, slotClaim{want: want, dryRun: preview})
	})
}

// slotClaim is what a claim asks for: the slot named by --slot (nil for the
// lowest free one), and whether to only decide it.
type slotClaim struct {
	want   *int
	dryRun bool
}

// claimLease decides the Port slot of the working copy c and, unless
// sc.dryRun, writes its lease, all under the meta-root lock: *sc.want when
// given (--slot), else the slot the copy holds, else the lowest free slot
// whose ports have no listener.
func claimLease(r Reporter, ws *project.Workspace, c workingCopy, sc slotClaim) (leaseChange, error) {
	policy, err := portPolicy(c.cfg, ws)
	if err != nil {
		return leaseChange{}, err
	}
	unlock, err := ports.LockMeta(ws.MetaRoot)
	if err != nil {
		return leaseChange{}, clierr.Failuref("%w; no lease was written", err)
	}
	defer unlock()
	copies, err := ws.EnumerateCopies()
	if err != nil {
		return leaseChange{}, clierr.Failuref("list working copies: %w; no lease was written", err)
	}
	held, err := ports.ReadHoldings(copies)
	if err != nil {
		return leaseChange{}, clierr.Failuref("%w; no lease was written", err)
	}
	name := c.name()
	var ch leaseChange
	for _, h := range held {
		if h.Copy == name {
			ch.previous = h.Slot
		}
	}
	if sc.want != nil {
		ch.slot = *sc.want
		err = ports.ClaimSlot(held, name, ch.slot, policy.Limit)
	} else {
		ch.slot, err = ports.Claim(held, name, policy.Limit, listenerProbe(r, policy))
	}
	if err != nil {
		return leaseChange{}, claimError(err)
	}
	ch.plan = fmt.Sprintf("would claim slot %d for %s", ch.slot, name)
	if sc.dryRun {
		return ch, nil
	}
	if err := ports.WriteLease(c.path, ports.NewExports(c.cfg, c.metaRoot, name, ch.slot)); err != nil {
		return leaseChange{}, clierr.Failuref("%w; the copy is still on %s", err, slotLabel(ch.previous))
	}
	return ch, nil
}

// listeners returns which of the given ports have a listener. It is a
// variable so tests can script the host's sockets.
var listeners = ports.Listeners

// listenerProbe is the busy test of a claim without --slot: a slot is busy
// while any of its ports has a listener, which it warns about on r.
func listenerProbe(r Reporter, policy config.PortPolicy) ports.Busy {
	return func(slot int) (bool, error) {
		listening, err := listeners(ports.SlotPorts(policy, slot))
		if err != nil {
			return false, &probeError{err: err}
		}
		if len(listening) > 0 {
			r.Warn(fmt.Sprintf("slot %d skipped: %s %s a listener; `--slot %d` takes it anyway",
				slot, portList(listening), plural(len(listening), "has", "have"), slot))
		}
		return len(listening) > 0, nil
	}
}

// probeError is a listener probe that could not run.
type probeError struct{ err error }

func (e *probeError) Error() string { return e.err.Error() }
func (e *probeError) Unwrap() error { return e.err }

// claimError tags an allocation refusal with its exit code: a held slot or
// conflicting leases are refused (3), a slot out of range is a usage error,
// and a failed listener probe names --slot, which skips it.
func claimError(err error) error {
	var conflict *ports.ConflictError
	var held *ports.SlotHeldError
	var outOfRange *ports.SlotRangeError
	var probe *probeError
	switch {
	case errors.As(err, &conflict), errors.As(err, &held):
		return clierr.Refusedf("%w; nothing was changed", err)
	case errors.As(err, &outOfRange):
		return usagef("%w", err)
	case errors.As(err, &probe):
		return clierr.Failuref("%w; no lease was written. Pick a slot with --slot N, which skips the probe", err)
	default:
		return clierr.Failuref("%w; nothing was changed", err)
	}
}

// portList spells ports as "port 3010" or "ports 3010, 54332".
func portList(list []int) string {
	spelled := make([]string, len(list))
	for i, p := range list {
		spelled[i] = strconv.Itoa(p)
	}
	return plural(len(list), "port ", "ports ") + strings.Join(spelled, ", ")
}

func runPortsRelease(ctx context.Context, o output, name string, req leaseRequest) error {
	_, c, err := portsTarget(o, name, "mws ports release <copy>")
	if err != nil {
		return err
	}
	held, err := ports.ReadSlot(c.path)
	if err != nil {
		return o.fail(clierr.Failuref("%w", err))
	}
	if held == 0 {
		o.reporter().Info(fmt.Sprintf("%s is already on the default ports; nothing to release", c.name()))
		return o.result(portsChange{DryRun: req.dryRun, Copy: c.name(), Path: c.path, Files: []envFileResult{}}, func(w io.Writer) error {
			if req.dryRun {
				return nil
			}
			_, err := fmt.Fprintf(w, "%s %s\n", c.name(), slotLabel(0))
			return err
		})
	}
	req.invocation = "mws ports release " + c.name()
	return changeLease(ctx, o, c, req, func(_ Reporter, preview bool) (leaseChange, error) {
		if preview {
			return leaseChange{previous: held, plan: fmt.Sprintf("would release slot %d from %s", held, c.name())}, nil
		}
		previous, err := releaseLease(c.metaRoot, c.path)
		return leaseChange{previous: previous}, err
	})
}

// releaseLease removes the Port lease of the working copy at copyPath under
// the meta-root lock, putting it on the default ports, and returns the slot
// it held. It renders nothing; the caller re-renders the copy.
func releaseLease(metaRoot, copyPath string) (int, error) {
	unlock, err := ports.LockMeta(metaRoot)
	if err != nil {
		return 0, clierr.Failuref("%w; the lease was kept", err)
	}
	defer unlock()
	previous, err := ports.ReadSlot(copyPath)
	if err != nil {
		return 0, clierr.Failuref("%w", err)
	}
	if err := ports.RemoveLease(copyPath); err != nil {
		return 0, clierr.Failuref("%w; the copy still holds %s", err, slotLabel(previous))
	}
	return previous, nil
}

// finishPortsChange renders the working copy c in mode after its lease
// changed as change says (or, previewing, would change), and prints the
// result. invocation is the command line that repeats the run.
func finishPortsChange(ctx context.Context, o output, r Reporter, c workingCopy, mode renderMode, change leaseChange, invocation string) error {
	cr, setupErr := c.render(ctx, r, mode, o.stderr)
	dryRun := mode == renderPreview
	res := portsChange{DryRun: dryRun, Copy: cr.Copy, Path: c.path, Slot: change.slot, PreviousSlot: change.previous, Files: cr.Files, Error: cr.Error}
	if err := o.result(res, func(w io.Writer) error {
		if !dryRun {
			_, err := fmt.Fprintf(w, "%s %s\n", res.Copy, slotLabel(change.slot))
			return err
		}
		if _, err := fmt.Fprintln(w, change.plan); err != nil {
			return err
		}
		return writeWouldCopy(w, c.metaRoot, res.Files)
	}); err != nil {
		return err
	}
	renderErr := envRunResult{Copies: []envCopyResult{cr}}.failure(invocation)
	if err := errors.Join(renderErr, setupErr); err != nil {
		return clierr.Failuref("%w", err)
	}
	return nil
}
