package ports

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestClaimTakesLowestFreeSlot(t *testing.T) {
	held := []Holding{{"main", 0}, {"a", 1}, {"b", 3}, {"c", 0}}
	slot, err := Claim(held, "c", 10, nil)
	if err != nil || slot != 2 {
		t.Fatalf("Claim = %d, %v; want 2, nil", slot, err)
	}
}

func TestClaimKeepsASlottedCopysSlot(t *testing.T) {
	held := []Holding{{"a", 4}, {"b", 1}}
	slot, err := Claim(held, "a", 10, nil)
	if err != nil || slot != 4 {
		t.Fatalf("Claim = %d, %v; want 4, nil", slot, err)
	}
}

func TestClaimSkipsBusySlots(t *testing.T) {
	busy := func(slot int) (bool, error) { return slot == 1 || slot == 2, nil }
	slot, err := Claim([]Holding{{"a", 0}}, "a", 10, busy)
	if err != nil || slot != 3 {
		t.Fatalf("Claim = %d, %v; want 3, nil", slot, err)
	}
}

func TestClaimStopsAtAProbeError(t *testing.T) {
	probeErr := errors.New("probe failed")
	probed := 0
	busy := func(int) (bool, error) {
		probed++
		return false, probeErr
	}
	if _, err := Claim([]Holding{{"a", 0}}, "a", 10, busy); !errors.Is(err, probeErr) {
		t.Fatalf("Claim error = %v, want the probe's error", err)
	}
	if probed != 1 {
		t.Errorf("busy was called %d times, want the search to stop at the first error", probed)
	}
}

func TestClaimStopsAtTheLimit(t *testing.T) {
	held := []Holding{{"a", 1}, {"b", 2}, {"c", 0}}
	_, err := Claim(held, "c", 2, nil)
	var exhausted *ExhaustedError
	if !errors.As(err, &exhausted) || exhausted.Limit != 2 {
		t.Fatalf("Claim error = %v, want *ExhaustedError with limit 2", err)
	}
}

func TestClaimRefusesWhileTwoLeasesShareASlot(t *testing.T) {
	held := []Holding{{"a", 2}, {"b", 2}, {"c", 0}}
	_, err := Claim(held, "c", 10, nil)
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("Claim error = %v, want *ConflictError", err)
	}
	want := []Conflict{{Slot: 2, Copies: []string{"a", "b"}}}
	if !reflect.DeepEqual(conflict.Conflicts, want) {
		t.Fatalf("Conflicts = %+v, want %+v", conflict.Conflicts, want)
	}
}

func TestClaimSlotMovesOrSettles(t *testing.T) {
	held := []Holding{{"a", 2}, {"b", 2}, {"c", 5}}
	if err := ClaimSlot(held, "b", 7, 10); err != nil {
		t.Fatalf("ClaimSlot to a free slot during a conflict: %v", err)
	}
	if err := ClaimSlot(held, "c", 5, 10); err != nil {
		t.Fatalf("ClaimSlot to the copy's own slot: %v", err)
	}
}

func TestClaimSlotRefusesAHeldSlot(t *testing.T) {
	err := ClaimSlot([]Holding{{"a", 3}, {"b", 0}}, "b", 3, 10)
	var heldErr *SlotHeldError
	if !errors.As(err, &heldErr) || heldErr.Slot != 3 || heldErr.Holder != "a" {
		t.Fatalf("ClaimSlot error = %v, want *SlotHeldError{3, a}", err)
	}
}

func TestClaimSlotRefusesOutOfRange(t *testing.T) {
	for _, slot := range []int{0, -1, 11} {
		err := ClaimSlot(nil, "a", slot, 10)
		var rangeErr *SlotRangeError
		if !errors.As(err, &rangeErr) {
			t.Fatalf("ClaimSlot(%d) error = %v, want *SlotRangeError", slot, err)
		}
	}
}

func TestNextFree(t *testing.T) {
	slot, err := NextFree([]Holding{{"a", 1}, {"b", 2}}, 10, nil)
	if err != nil || slot != 3 {
		t.Fatalf("NextFree = %d, %v; want 3, nil", slot, err)
	}
}

func TestConflictsAreSortedBySlot(t *testing.T) {
	held := []Holding{{"d", 4}, {"c", 4}, {"a", 1}, {"b", 1}, {"e", 0}, {"f", 0}}
	want := []Conflict{{1, []string{"a", "b"}}, {4, []string{"c", "d"}}}
	if got := Conflicts(held); !reflect.DeepEqual(got, want) {
		t.Fatalf("Conflicts = %+v, want %+v", got, want)
	}
}

func TestReadHoldings(t *testing.T) {
	root := t.TempDir()
	one, two := filepath.Join(root, "one"), filepath.Join(root, "two")
	for _, d := range []string{one, two} {
		mkdir(t, d)
	}
	if err := WriteLease(two, Exports{Slot: 6, Vars: []Var{{"MWS_SLOT", "6"}}}); err != nil {
		t.Fatal(err)
	}
	held, err := ReadHoldings([]string{one, two})
	if err != nil {
		t.Fatalf("ReadHoldings: %v", err)
	}
	if want := []Holding{{"one", 0}, {"two", 6}}; !reflect.DeepEqual(held, want) {
		t.Fatalf("ReadHoldings = %+v, want %+v", held, want)
	}
}
