package ports

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// Holding is a live working copy's Port slot as its lease records it; slot 0
// is a copy on the default ports.
type Holding struct {
	// Copy is the working copy's directory name.
	Copy string
	// Slot is the leased slot, or 0 without a lease.
	Slot int
}

// Conflict is one slot held by more than one live copy, as after a copy is
// duplicated with `cp -r`.
type Conflict struct {
	Slot int
	// Copies holds the copies' names, sorted.
	Copies []string
}

// ConflictError refuses an allocation while leases conflict; `mws ports
// claim --slot N` settles one by moving a copy.
type ConflictError struct {
	Conflicts []Conflict
}

func (e *ConflictError) Error() string {
	parts := make([]string, len(e.Conflicts))
	for i, c := range e.Conflicts {
		parts[i] = fmt.Sprintf("slot %d is leased by %s", c.Slot, strings.Join(c.Copies, ", "))
	}
	return fmt.Sprintf("port leases conflict: %s; move all but one copy with `mws ports claim <copy> --slot N`", strings.Join(parts, "; "))
}

// SlotHeldError refuses a slot another live copy holds.
type SlotHeldError struct {
	Slot   int
	Holder string
}

func (e *SlotHeldError) Error() string {
	return fmt.Sprintf("slot %d is held by working copy %q; pick another slot, or run `mws ports release %s` first", e.Slot, e.Holder, e.Holder)
}

// SlotRangeError refuses a slot outside 1..Limit.
type SlotRangeError struct {
	Slot  int
	Limit int
}

func (e *SlotRangeError) Error() string {
	return fmt.Sprintf("slot %d is out of range; claimable slots are 1..%d (slot 0 is the default ports: use `mws ports release` instead)", e.Slot, e.Limit)
}

// ExhaustedError reports that every slot from 1 to Limit is held or busy.
type ExhaustedError struct {
	Limit int
	// Busy lists the free slots skipped because a port had a listener.
	Busy []int
}

func (e *ExhaustedError) Error() string {
	msg := fmt.Sprintf("no free port slot in 1..%d", e.Limit)
	if len(e.Busy) > 0 {
		msg += fmt.Sprintf(" (slots %v are free but have listeners)", e.Busy)
	}
	return msg + "; release a slot with `mws ports release <copy>`, or raise ports.slots in .mws.toml if it caps the count"
}

// ReadHoldings reads the lease of each working copy in copyDirs.
func ReadHoldings(copyDirs []string) ([]Holding, error) {
	held := make([]Holding, 0, len(copyDirs))
	for _, dir := range copyDirs {
		slot, err := ReadSlot(dir)
		if err != nil {
			return nil, err
		}
		held = append(held, Holding{Copy: filepath.Base(dir), Slot: slot})
	}
	return held, nil
}

// Conflicts lists every slot of at least 1 that more than one copy holds,
// sorted by slot.
func Conflicts(held []Holding) []Conflict {
	bySlot := map[int][]string{}
	for _, h := range held {
		if h.Slot > 0 {
			bySlot[h.Slot] = append(bySlot[h.Slot], h.Copy)
		}
	}
	var out []Conflict
	for slot, copies := range bySlot {
		if len(copies) > 1 {
			slices.Sort(copies)
			out = append(out, Conflict{Slot: slot, Copies: copies})
		}
	}
	slices.SortFunc(out, func(a, b Conflict) int { return a.Slot - b.Slot })
	return out
}

// NextFree returns the lowest slot in 1..limit that no copy holds and busy
// does not reject. busy, typically a listener probe, may be nil. When none
// is left the error is an *ExhaustedError.
func NextFree(held []Holding, limit int, busy func(slot int) bool) (int, error) {
	var skipped []int
	for slot := 1; slot <= limit; slot++ {
		if isHeld(held, slot) {
			continue
		}
		if busy != nil && busy(slot) {
			skipped = append(skipped, slot)
			continue
		}
		return slot, nil
	}
	return 0, &ExhaustedError{Limit: limit, Busy: skipped}
}

// Claim decides the slot for the copy named copyName given every live
// copy's holding: its current slot when it holds one, otherwise NextFree.
// While any two leases conflict it refuses with a *ConflictError.
func Claim(held []Holding, copyName string, limit int, busy func(slot int) bool) (int, error) {
	if conflicts := Conflicts(held); len(conflicts) > 0 {
		return 0, &ConflictError{Conflicts: conflicts}
	}
	for _, h := range held {
		if h.Copy == copyName && h.Slot > 0 {
			return h.Slot, nil
		}
	}
	return NextFree(held, limit, busy)
}

// ClaimSlot checks that the copy named copyName may take or move to slot:
// it must be in 1..limit (*SlotRangeError) and no other copy may hold it
// (*SlotHeldError). An explicit slot overrides the listener probe and is
// how a conflict is settled.
func ClaimSlot(held []Holding, copyName string, slot, limit int) error {
	if slot < 1 || slot > limit {
		return &SlotRangeError{Slot: slot, Limit: limit}
	}
	for _, h := range held {
		if h.Slot == slot && h.Copy != copyName {
			return &SlotHeldError{Slot: slot, Holder: h.Copy}
		}
	}
	return nil
}

// isHeld reports whether any copy holds slot.
func isHeld(held []Holding, slot int) bool {
	return slices.ContainsFunc(held, func(h Holding) bool { return h.Slot == slot })
}
