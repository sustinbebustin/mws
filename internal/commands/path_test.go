package commands

import "testing"

func TestPathPrintsTheCopysAbsolutePath(t *testing.T) {
	f := seedWtWorkspace(t)

	res := mustMWS(t, f.meta, "path", "c")
	if got, want := res.stdout, f.copy+"\n"; got != want {
		t.Errorf("path c stdout = %q, want %q", got, want)
	}

	inside := mustMWS(t, f.repo("frontend"), "path")
	if got, want := inside.stdout, f.copy+"\n"; got != want {
		t.Errorf("path from inside the copy = %q, want %q", got, want)
	}
}

func TestPathJSONPrintsNameAndPath(t *testing.T) {
	f := seedWtWorkspace(t)

	var got struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	decodeJSON(t, mustMWS(t, f.meta, "path", "c", "--json"), &got)

	if got.Name != "c" || got.Path != f.copy {
		t.Errorf("path --json = %+v, want name c, path %s", got, f.copy)
	}
}

func TestPathOfAnUnknownCopyExitsFour(t *testing.T) {
	f := seedWtWorkspace(t)

	res := runMWS(t, f.meta, "path", "nope")
	if res.code != 4 {
		t.Fatalf("path nope exit = %d, want 4\nstderr: %s", res.code, res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("path nope printed on stdout: %q", res.stdout)
	}

	res = runMWS(t, f.meta, "path", "nope", "--json")
	if res.code != 4 {
		t.Fatalf("path nope --json exit = %d, want 4", res.code)
	}
	if e := decodeError(t, res); e.Code != "not_found" {
		t.Errorf("error code = %q, want not_found", e.Code)
	}
}

func TestPathWithNoCopyNamedOutsideACopyIsAUsageError(t *testing.T) {
	f := seedWtWorkspace(t)

	if res := runMWS(t, f.meta, "path"); res.code != 2 {
		t.Fatalf("path with no copy exit = %d, want 2\nstderr: %s", res.code, res.stderr)
	}
}
