package ports

import (
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// claimLocked is a command's read-decide-write: lock the meta root, read the
// live copies' leases, pick a slot, write the lease.
func claimLocked(meta string, copies []string, copyDir string) (int, error) {
	unlock, err := LockMeta(meta)
	if err != nil {
		return 0, err
	}
	defer unlock()
	held, err := readHoldings(copies)
	if err != nil {
		return 0, err
	}
	slot, err := Claim(held, filepath.Base(copyDir), 10, nil)
	if err != nil {
		return 0, err
	}
	// Widen the window between deciding and writing, where an unlocked
	// allocator would hand both claimants the same slot.
	time.Sleep(50 * time.Millisecond)
	return slot, WriteLease(copyDir, Exports{Slot: slot, Vars: []Var{{"MWS_SLOT", strconv.Itoa(slot)}}})
}

func TestLockMetaSerialisesConcurrentAllocations(t *testing.T) {
	meta := t.TempDir()
	copies := []string{filepath.Join(meta, "a"), filepath.Join(meta, "b")}
	for _, c := range copies {
		mkdir(t, c)
	}
	slots := make([]int, len(copies))
	errs := make([]error, len(copies))
	var wg sync.WaitGroup
	for i, c := range copies {
		wg.Go(func() { slots[i], errs[i] = claimLocked(meta, copies, c) })
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
	}
	if slots[0] == slots[1] {
		t.Fatalf("both copies got slot %d", slots[0])
	}
	held, err := readHoldings(copies)
	if err != nil {
		t.Fatal(err)
	}
	if len(Conflicts(held)) != 0 {
		t.Fatalf("leases conflict after concurrent claims: %+v", held)
	}
}
