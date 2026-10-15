package commands

import (
	"net"
	"path/filepath"
	"testing"

	"github.com/sustinbebustin/mws/internal/config"
	"github.com/sustinbebustin/mws/internal/ports"
)

// withFreePort gives the env fixture a [ports] table with one port, web
// aliased PORT at stride 10, whose base the OS just handed out as free, so
// the listener probe of a claim does not trip over the host's own services.
// It returns the base.
func (f envFixture) withFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not TCP", l.Addr())
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(f.meta)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Ports = &config.Ports{Port: []config.Port{{Name: "web", Base: addr.Port, Var: "PORT"}}}
	if err := config.Save(f.meta, cfg); err != nil {
		t.Fatal(err)
	}
	return addr.Port
}

// slotOf reads the Port slot the lease of the working copy at dir records.
func slotOf(t *testing.T, dir string) int {
	t.Helper()
	slot, err := ports.ReadSlot(dir)
	if err != nil {
		t.Fatal(err)
	}
	return slot
}

// mustPorts runs `mws ports <args>` from cwd and fails the test unless it
// exits 0.
func mustPorts(t *testing.T, cwd string, args ...string) mwsResult {
	t.Helper()
	res := runMWS(t, cwd, append([]string{"ports"}, args...)...)
	if res.code != 0 {
		t.Fatalf("mws ports %v: exit %d\nstdout: %s\nstderr: %s", args, res.code, res.stdout, res.stderr)
	}
	return res
}

func TestPortsClaimTakesTheLowestFreeSlotAndReleaseGivesItBack(t *testing.T) {
	f := seedEnvWorkspace(t)
	f.withFreePort(t)
	mustMkdir(t, filepath.Join(f.copy("c"), "frontend"))

	mustPorts(t, f.meta, "claim", "a")
	mustPorts(t, f.meta, "claim", "b")
	if got := []int{slotOf(t, f.copy("a")), slotOf(t, f.copy("b"))}; got[0] != 1 || got[1] != 2 {
		t.Fatalf("slots of a, b = %v, want [1 2]", got)
	}

	mustPorts(t, f.meta, "release", "a")
	if got := slotOf(t, f.copy("a")); got != 0 {
		t.Fatalf("after release a is on slot %d, want 0", got)
	}
	mustPorts(t, f.meta, "claim", "c")
	if got := slotOf(t, f.copy("c")); got != 1 {
		t.Fatalf("c claimed slot %d, want the released 1", got)
	}

	// A retry keeps the slot.
	mustPorts(t, f.meta, "claim", "b")
	if got := slotOf(t, f.copy("b")); got != 2 {
		t.Fatalf("re-claim moved b to slot %d, want it kept on 2", got)
	}
}
