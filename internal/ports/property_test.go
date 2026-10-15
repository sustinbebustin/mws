package ports

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"pgregory.net/rapid"

	"github.com/sustinbebustin/mws/internal/config"
)

// leaseMachine drives claim, move, release, remove, restore and add over
// real copy directories and their lease files, against a model of which
// copy holds which slot. Removal releases before trashing, as `mws rm` does.
type leaseMachine struct {
	copiesRoot string
	trashRoot  string
	limit      int
	// live maps each live copy to its slot; 0 is the default ports.
	live map[string]int
	// trashed holds removed copies' names.
	trashed map[string]bool
}

var copyNames = []string{"main", "a", "b", "c", "d", "e", "f", "g"}

func (m *leaseMachine) copyDir(name string) string { return filepath.Join(m.copiesRoot, name) }

func (m *leaseMachine) liveNames() []string {
	names := make([]string, 0, len(m.live))
	for n := range m.live {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

func (m *leaseMachine) holdings(t *rapid.T) []Holding {
	dirs := make([]string, 0, len(m.live))
	for _, n := range m.liveNames() {
		dirs = append(dirs, m.copyDir(n))
	}
	held, err := ReadHoldings(dirs)
	if err != nil {
		t.Fatalf("ReadHoldings: %v", err)
	}
	return held
}

// lowestFree is the model's answer: the smallest slot in 1..limit no live
// copy holds, or 0 when every slot is taken.
func (m *leaseMachine) lowestFree() int {
	taken := map[int]bool{}
	for _, s := range m.live {
		taken[s] = true
	}
	for s := 1; s <= m.limit; s++ {
		if !taken[s] {
			return s
		}
	}
	return 0
}

func (m *leaseMachine) write(t *rapid.T, name string, slot int) {
	if err := WriteLease(m.copyDir(name), Exports{Slot: slot, Vars: []Var{{config.VarSlot, strconv.Itoa(slot)}}}); err != nil {
		t.Fatalf("WriteLease(%s, %d): %v", name, slot, err)
	}
	m.live[name] = slot
}

func (m *leaseMachine) release(t *rapid.T, name string) {
	if err := RemoveLease(m.copyDir(name)); err != nil {
		t.Fatalf("RemoveLease(%s): %v", name, err)
	}
	m.live[name] = 0
}

// claim runs Claim for name and checks it against the model: a slotted copy
// keeps its slot, an unslotted one gets the lowest free slot, and a full
// workspace is an *ExhaustedError.
func (m *leaseMachine) claim(t *rapid.T, name string) {
	before := m.live[name]
	want := before
	if want == 0 {
		want = m.lowestFree()
	}
	slot, err := Claim(m.holdings(t), name, m.limit, nil)
	if want == 0 {
		var exhausted *ExhaustedError
		if !errors.As(err, &exhausted) {
			t.Fatalf("Claim(%s) with every slot taken = %d, %v; want *ExhaustedError", name, slot, err)
		}
		return
	}
	if err != nil || slot != want {
		t.Fatalf("Claim(%s) on slot %d = %d, %v; want %d", name, before, slot, err, want)
	}
	m.write(t, name, slot)
}

func (m *leaseMachine) pickLive(t *rapid.T) string {
	names := m.liveNames()
	if len(names) == 0 {
		t.Skip("no live copy")
	}
	return rapid.SampledFrom(names).Draw(t, "copy")
}

func (m *leaseMachine) Add(t *rapid.T) {
	name := rapid.SampledFrom(copyNames).Draw(t, "name")
	if _, ok := m.live[name]; ok || m.trashed[name] {
		t.Skip("name taken")
	}
	if err := os.Mkdir(m.copyDir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	m.live[name] = 0
}

func (m *leaseMachine) Claim(t *rapid.T) { m.claim(t, m.pickLive(t)) }

func (m *leaseMachine) Move(t *rapid.T) {
	name := m.pickLive(t)
	slot := rapid.IntRange(-1, m.limit+1).Draw(t, "slot")
	err := ClaimSlot(m.holdings(t), name, slot, m.limit)
	holder := ""
	for n, s := range m.live {
		if s == slot && n != name {
			holder = n
		}
	}
	var rangeErr *SlotRangeError
	var heldErr *SlotHeldError
	switch {
	case slot < 1 || slot > m.limit:
		if !errors.As(err, &rangeErr) {
			t.Fatalf("ClaimSlot(%s, %d) = %v; want *SlotRangeError", name, slot, err)
		}
	case holder != "":
		if !errors.As(err, &heldErr) || heldErr.Holder != holder {
			t.Fatalf("ClaimSlot(%s, %d) = %v; want *SlotHeldError by %s", name, slot, err, holder)
		}
	case err != nil:
		t.Fatalf("ClaimSlot(%s, %d) on a free slot: %v", name, slot, err)
	default:
		m.write(t, name, slot)
	}
}

func (m *leaseMachine) Release(t *rapid.T) { m.release(t, m.pickLive(t)) }

// ReleaseThenClaim checks that a released copy claims the lowest free slot.
func (m *leaseMachine) ReleaseThenClaim(t *rapid.T) {
	name := m.pickLive(t)
	m.release(t, name)
	want := m.lowestFree()
	m.claim(t, name)
	if want != 0 && m.live[name] != want {
		t.Fatalf("release then claim of %s gave slot %d, want lowest free %d", name, m.live[name], want)
	}
}

func (m *leaseMachine) Remove(t *rapid.T) {
	name := m.pickLive(t)
	m.release(t, name)
	if err := os.Rename(m.copyDir(name), filepath.Join(m.trashRoot, name)); err != nil {
		t.Fatal(err)
	}
	delete(m.live, name)
	m.trashed[name] = true
}

func (m *leaseMachine) Restore(t *rapid.T) {
	var names []string
	for n := range m.trashed {
		names = append(names, n)
	}
	if len(names) == 0 {
		t.Skip("trash is empty")
	}
	slices.Sort(names)
	name := rapid.SampledFrom(names).Draw(t, "trashed")
	if err := os.Rename(filepath.Join(m.trashRoot, name), m.copyDir(name)); err != nil {
		t.Fatal(err)
	}
	delete(m.trashed, name)
	m.live[name] = 0
}

// Check asserts the invariants after every action: the leases on disk match
// the model, no two live leases share a slot, none is 0, none exceeds the
// limit, and the trash holds no lease.
func (m *leaseMachine) Check(t *rapid.T) {
	held := m.holdings(t)
	if c := Conflicts(held); len(c) > 0 {
		t.Fatalf("live leases conflict: %+v", c)
	}
	for _, h := range held {
		if h.Slot != m.live[h.Copy] {
			t.Fatalf("copy %s: lease slot %d, model %d", h.Copy, h.Slot, m.live[h.Copy])
		}
		if h.Slot < 0 || h.Slot > m.limit {
			t.Fatalf("copy %s holds slot %d outside 0..%d", h.Copy, h.Slot, m.limit)
		}
		_, err := os.Stat(LeasePath(m.copyDir(h.Copy)))
		if hasLease := err == nil; hasLease != (h.Slot > 0) {
			t.Fatalf("copy %s on slot %d has lease file = %v", h.Copy, h.Slot, hasLease)
		}
	}
	for name := range m.trashed {
		if _, err := os.Stat(LeasePath(filepath.Join(m.trashRoot, name))); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("trashed copy %s holds a lease (stat: %v)", name, err)
		}
	}
}

func TestPropertyLeaseInvariants(t *testing.T) {
	base := t.TempDir()
	rapid.Check(t, func(t *rapid.T) {
		root, err := os.MkdirTemp(base, "meta-")
		if err != nil {
			t.Fatal(err)
		}
		m := &leaseMachine{
			copiesRoot: filepath.Join(root, "copies"),
			trashRoot:  filepath.Join(root, "trash"),
			limit:      rapid.IntRange(1, 5).Draw(t, "limit"),
			live:       map[string]int{},
			trashed:    map[string]bool{},
		}
		for _, d := range []string{m.copiesRoot, m.trashRoot} {
			if err := os.Mkdir(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		t.Repeat(rapid.StateMachineActions(m))
	})
}

// TestPropertyAllocationStaysCollisionFree draws a random valid [ports]
// table and a set of held slots, and checks that a claimed slot is within
// the limit, keeps every port at or below 65535, and shares no port with
// the default ports or any held slot.
func TestPropertyAllocationStaysCollisionFree(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		stride := rapid.IntRange(1, 50).Draw(t, "stride")
		n := rapid.IntRange(1, 6).Draw(t, "ports")
		ports := &config.Ports{Stride: &stride}
		for i := range n {
			ports.Port = append(ports.Port, config.Port{
				Name: fmt.Sprintf("p%d", i),
				Base: rapid.IntRange(1, config.MaxPort).Draw(t, "base"),
			})
		}
		if rapid.Bool().Draw(t, "capped") {
			slots := rapid.IntRange(1, 30).Draw(t, "slots")
			ports.Slots = &slots
		}
		cfg := &config.Config{Ports: ports}
		if cfg.Validate() != nil {
			t.Skip("invalid [ports] table")
		}
		policy, ok := cfg.PortPolicy()
		if !ok {
			t.Fatal("PortPolicy reported no [ports]")
		}
		if policy.Limit < 1 {
			t.Skip("no claimable slot")
		}
		var held []Holding
		taken := map[int]bool{}
		for i, s := range rapid.SliceOfNDistinct(rapid.IntRange(1, policy.Limit), 0, 20, rapid.ID).Draw(t, "held") {
			held = append(held, Holding{Copy: fmt.Sprintf("c%d", i), Slot: s})
			taken[s] = true
		}
		slot, err := Claim(held, "new", policy.Limit, nil)
		if err != nil {
			var exhausted *ExhaustedError
			if !errors.As(err, &exhausted) || len(taken) < policy.Limit {
				t.Fatalf("Claim = %v with %d of %d slots held", err, len(taken), policy.Limit)
			}
			return
		}
		if slot < 1 || slot > policy.Limit {
			t.Fatalf("Claim = %d, outside 1..%d", slot, policy.Limit)
		}
		mine := SlotPorts(policy, slot)
		for _, p := range mine {
			if p > config.MaxPort {
				t.Fatalf("slot %d port %d exceeds %d", slot, p, config.MaxPort)
			}
		}
		for other := range taken {
			for _, p := range SlotPorts(policy, other) {
				if slices.Contains(mine, p) {
					t.Fatalf("slot %d shares port %d with held slot %d", slot, p, other)
				}
			}
		}
		for _, p := range SlotPorts(policy, 0) {
			if slices.Contains(mine, p) {
				t.Fatalf("slot %d shares port %d with the default ports", slot, p)
			}
		}
	})
}

// renderToken is one piece of a generated source: literal text, or a
// ${NAME} placeholder.
type renderToken struct {
	text     string
	exported bool
	name     string
}

var renderExportNames = []string{"MWS_SLOT", "MWS_PORT_WEB", "WEB_PORT", "DATABASE_URL"}

func renderExports(slot int) Exports {
	return Exports{Slot: slot, Vars: []Var{
		{"MWS_SLOT", strconv.Itoa(slot)},
		{"MWS_PORT_WEB", strconv.Itoa(5173 + 10*slot)},
		{"WEB_PORT", strconv.Itoa(5173 + 10*slot)},
		{"DATABASE_URL", fmt.Sprintf("postgres://localhost:%d/x", 5432+10*slot)},
	}}
}

// renderTokenGen draws literals that hold '$', '{' and names but no '}', so
// no literal can complete a placeholder; '}' appears only inside a token.
func renderTokenGen() *rapid.Generator[renderToken] {
	literal := rapid.Custom(func(t *rapid.T) renderToken {
		s := rapid.StringOf(rapid.SampledFrom([]rune("ab_AZ09$ {\n=:\x00é"))).Draw(t, "literal")
		return renderToken{text: s}
	})
	exported := rapid.Custom(func(t *rapid.T) renderToken {
		name := rapid.SampledFrom(renderExportNames).Draw(t, "exported")
		return renderToken{text: "${" + name + "}", exported: true, name: name}
	})
	other := rapid.Custom(func(t *rapid.T) renderToken {
		name := rapid.SampledFrom([]string{"lower", "MWS_PORT_TYPO", "Mixed", "HOME"}).Draw(t, "other")
		return renderToken{text: rapid.SampledFrom([]string{"${" + name + "}", "$" + name}).Draw(t, "form")}
	})
	return rapid.OneOf(literal, exported, other)
}

// TestPropertyRenderTouchesOnlyExportedPlaceholders: every byte outside a
// ${VAR} for an exported VAR survives, each such placeholder becomes its
// value, and rendering is pure, so rendering at slot k first does not
// change the slot-0 output.
func TestPropertyRenderTouchesOnlyExportedPlaceholders(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		tokens := rapid.SliceOf(renderTokenGen()).Draw(t, "tokens")
		k := rapid.IntRange(0, 100).Draw(t, "slot")
		e := renderExports(k)
		var src, want bytes.Buffer
		for _, tok := range tokens {
			src.WriteString(tok.text)
			if tok.exported {
				v, _ := e.Lookup(tok.name)
				want.WriteString(v)
			} else {
				want.WriteString(tok.text)
			}
		}
		got, _ := Render(src.Bytes(), e)
		if !bytes.Equal(got, want.Bytes()) {
			t.Fatalf("Render(%q) =\n %q\nwant\n %q", src.Bytes(), got, want.Bytes())
		}
		fresh, _ := Render(src.Bytes(), renderExports(0))
		Render(src.Bytes(), e)
		again, _ := Render(src.Bytes(), renderExports(0))
		if !bytes.Equal(fresh, again) {
			t.Fatalf("slot-0 render changed after a slot-%d render: %q vs %q", k, fresh, again)
		}
	})
}

// TestPropertyRenderWithoutPlaceholdersIsIdentity: a source with no
// placeholder for an exported name renders, at slot 0, to itself.
func TestPropertyRenderWithoutPlaceholdersIsIdentity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		src := rapid.SliceOf(rapid.SampledFrom([]byte("${}AZ_ab$\n\x00\xff"))).Draw(t, "src")
		for _, name := range renderExportNames {
			if bytes.Contains(src, []byte("${"+name+"}")) {
				t.Skip("source holds an exported placeholder")
			}
		}
		got, _ := Render(src, renderExports(0))
		if !bytes.Equal(got, src) {
			t.Fatalf("Render(%q) = %q, want the source unchanged", src, got)
		}
	})
}
