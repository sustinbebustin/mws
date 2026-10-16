package commands

import (
	"path/filepath"
	"slices"
	"testing"
)

type rmJSON struct {
	DryRun       bool   `json:"dry_run"`
	Copy         string `json:"copy"`
	Path         string `json:"path"`
	Entry        string `json:"entry"`
	Purged       bool   `json:"purged"`
	ReleasedSlot int    `json:"released_slot"`
}

type restoreJSON struct {
	Copy  string `json:"copy"`
	Path  string `json:"path"`
	Entry string `json:"entry"`
}

type cloneJSON struct {
	Copy  string   `json:"copy"`
	Path  string   `json:"path"`
	Repos []string `json:"repos"`
	Setup string   `json:"setup"`
	Ports string   `json:"ports"`
}

func TestRmJSONNamesTheTrashEntryAndTheReleasedSlot(t *testing.T) {
	f := seedWtWorkspace(t)
	f.withPortTemplate(t)
	mustMWS(t, f.meta, "ports", "claim", "c", "--slot", "2", "--no-setup")

	var got rmJSON
	decodeJSON(t, mustMWS(t, f.meta, "rm", "c", "--yes", "--json"), &got)

	want := rmJSON{Copy: "c", Path: f.copy, Entry: filepath.Base(filepath.Dir(trashedCopy(t, f.meta, "c"))), ReleasedSlot: 2}
	if got != want {
		t.Errorf("rm --json = %+v, want %+v", got, want)
	}
}

func TestRmJSONOfACopyOnTheDefaultsReleasesNothing(t *testing.T) {
	f := seedWtWorkspace(t)

	var got rmJSON
	decodeJSON(t, mustMWS(t, f.meta, "rm", "c", "--yes", "--json"), &got)

	if got.ReleasedSlot != 0 || got.Entry == "" || got.Purged {
		t.Errorf("rm --json = %+v, want an entry, no purge, no released slot", got)
	}
}

func TestRmPurgeJSONSaysPurgedWithoutAnEntry(t *testing.T) {
	f := seedWtWorkspace(t)

	var got rmJSON
	decodeJSON(t, mustMWS(t, f.meta, "rm", "c", "--purge", "--yes", "--json"), &got)

	if want := (rmJSON{Copy: "c", Path: f.copy, Purged: true}); got != want {
		t.Errorf("rm --purge --json = %+v, want %+v", got, want)
	}
}

func TestRmPurgeJSONNamesTheReleasedSlot(t *testing.T) {
	f := seedWtWorkspace(t)
	f.withPortTemplate(t)
	mustMWS(t, f.meta, "ports", "claim", "c", "--slot", "2", "--no-setup")

	var dry rmJSON
	decodeJSON(t, mustMWS(t, f.meta, "rm", "c", "--purge", "--dry-run", "--json"), &dry)
	if want := (rmJSON{DryRun: true, Copy: "c", Path: f.copy, Purged: true, ReleasedSlot: 2}); dry != want {
		t.Errorf("rm --purge --dry-run --json = %+v, want %+v", dry, want)
	}

	var got rmJSON
	decodeJSON(t, mustMWS(t, f.meta, "rm", "c", "--purge", "--yes", "--json"), &got)
	if want := (rmJSON{Copy: "c", Path: f.copy, Purged: true, ReleasedSlot: 2}); got != want {
		t.Errorf("rm --purge --json = %+v, want %+v", got, want)
	}
}

func TestRmDryRunJSONChangesNothing(t *testing.T) {
	f := seedWtWorkspace(t)
	f.withPortTemplate(t)
	mustMWS(t, f.meta, "ports", "claim", "c", "--slot", "2", "--no-setup")

	var got rmJSON
	decodeJSON(t, mustMWS(t, f.meta, "rm", "c", "--dry-run", "--json"), &got)

	if want := (rmJSON{DryRun: true, Copy: "c", Path: f.copy, ReleasedSlot: 2}); got != want {
		t.Errorf("rm --dry-run --json = %+v, want %+v", got, want)
	}
	if got := slotOf(t, f.copy); got != 2 {
		t.Errorf("dry run moved c to slot %d", got)
	}
}

func TestRmJSONFailuresPrintTheErrorObject(t *testing.T) {
	f := seedWtWorkspace(t)

	res := runMWS(t, f.meta, "rm", "nope", "--yes", "--json")
	if res.code != 4 {
		t.Fatalf("rm nope exit = %d, want 4", res.code)
	}
	if e := decodeError(t, res); e.Code != "not_found" || e.Message == "" {
		t.Errorf("error = %+v, want code not_found and a message", e)
	}

	res = runMWS(t, f.meta, "rm", "c", "--json")
	if res.code != 2 {
		t.Fatalf("rm without --yes exit = %d, want 2", res.code)
	}
	if e := decodeError(t, res); e.Code != "usage" {
		t.Errorf("error code = %q, want usage", e.Code)
	}
	if !fileExists(f.copy) {
		t.Error("a failed rm removed the copy")
	}
}

func TestRestoreJSONNamesThePathAndTheEntry(t *testing.T) {
	f := seedWtWorkspace(t)
	mustMWS(t, f.meta, "rm", "c", "--yes")
	entry := filepath.Base(filepath.Dir(trashedCopy(t, f.meta, "c")))

	var got restoreJSON
	decodeJSON(t, mustMWS(t, f.meta, "restore", "c", "--yes", "--json"), &got)

	if want := (restoreJSON{Copy: "c", Path: f.copy, Entry: entry}); got != want {
		t.Errorf("restore --json = %+v, want %+v", got, want)
	}

	mustMWS(t, f.meta, "rm", "c", "--yes")
	decodeJSON(t, mustMWS(t, f.meta, "restore", "c", "--as", "d", "--yes", "--json"), &got)
	if want := filepath.Join(f.meta, "d"); got.Path != want || got.Copy != "d" {
		t.Errorf("restore --as d --json = %+v, want path %s", got, want)
	}
}

func TestRestoreJSONFailuresPrintTheErrorObject(t *testing.T) {
	f := seedWtWorkspace(t)

	res := runMWS(t, f.meta, "restore", "nope", "--yes", "--json")
	if res.code != 4 {
		t.Fatalf("restore nope exit = %d, want 4", res.code)
	}
	if e := decodeError(t, res); e.Code != "not_found" {
		t.Errorf("error code = %q, want not_found", e.Code)
	}
}

func TestCloneJSONReportsPathReposSetupAndPorts(t *testing.T) {
	f := seedWtWorkspace(t)
	f.withPortTemplate(t)

	var got cloneJSON
	decodeJSON(t, mustMWS(t, f.meta, "clone", "d", "--ports", "--no-optional", "--setup", "--json"), &got)

	want := cloneJSON{Copy: "d", Path: filepath.Join(f.meta, "d"), Repos: []string{"backend", "frontend"}, Setup: "ran", Ports: "slot 1"}
	if got.Copy != want.Copy || got.Path != want.Path || got.Setup != want.Setup || got.Ports != want.Ports || !slices.Equal(got.Repos, want.Repos) {
		t.Errorf("clone --json = %+v, want %+v", got, want)
	}
}

func TestCloneJSONSetupOutcomes(t *testing.T) {
	f := seedWtWorkspace(t)

	var got cloneJSON
	decodeJSON(t, mustMWS(t, f.meta, "clone", "d", "--no-optional", "--no-setup", "--json"), &got)
	if got.Setup != "none" || got.Ports != "default" {
		t.Errorf("no setup configured: setup %q ports %q, want none and default", got.Setup, got.Ports)
	}

	f.withPortTemplate(t)
	decodeJSON(t, mustMWS(t, f.meta, "clone", "e", "--no-optional", "--no-setup", "--json"), &got)
	if got.Setup != "skipped" {
		t.Errorf("--no-setup with setup configured: setup %q, want skipped", got.Setup)
	}
}

func TestCloneJSONOfACompleteCopyClonesNoRepos(t *testing.T) {
	f := seedWtWorkspace(t)

	var got cloneJSON
	decodeJSON(t, mustMWS(t, f.meta, "clone", "c", "--with", "extra", "--no-setup", "--json"), &got)
	if want := []string{"extra"}; !slices.Equal(got.Repos, want) {
		t.Fatalf("repos = %v, want %v", got.Repos, want)
	}

	decodeJSON(t, mustMWS(t, f.meta, "clone", "c", "--with", "extra", "--no-setup", "--json"), &got)
	if got.Repos == nil || len(got.Repos) != 0 || got.Path != f.copy {
		t.Errorf("re-run = %+v, want path %s and an empty, non-null repos list", got, f.copy)
	}
}

func TestCloneJSONFailuresPrintTheErrorObject(t *testing.T) {
	f := seedWtWorkspace(t)

	res := runMWS(t, f.meta, "clone", "bad/name", "--no-optional", "--no-setup", "--json")
	if res.code != 2 {
		t.Fatalf("clone bad/name exit = %d, want 2\n%s", res.code, res.stderr)
	}
	if e := decodeError(t, res); e.Code != "usage" {
		t.Errorf("error code = %q, want usage", e.Code)
	}

	res = runMWS(t, f.meta, "clone", "d", "--json")
	if res.code != 2 {
		t.Fatalf("clone with unresolved optional repos exit = %d, want 2", res.code)
	}
	decodeError(t, res)
}
