package ports

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/sustinbebustin/mws/internal/config"
)

// Var is one exported variable.
type Var struct {
	Name  string
	Value string
}

// Exports is the export set of a working copy at a slot: what its Port lease
// lists, what a Render substitutes, and what setup commands see.
type Exports struct {
	// Slot is the copy's Port slot; 0 is the default ports.
	Slot int
	// Vars is the export set in a deterministic order.
	Vars []Var
}

// NewExports computes the export set of the working copy named copyName in
// the meta workspace at metaRoot, at slot. The order is fixed: the builtins
// (MWS_SLOT, MWS_COPY, MWS_COPY_SLUG, MWS_META), then each port in
// declaration order as MWS_PORT_<NAME> followed by its alias, then the
// `[vars]` entries sorted by name with their references substituted. cfg
// must have passed Validate, so every var reference resolves.
func NewExports(cfg *config.Config, metaRoot, copyName string, slot int) Exports {
	e := Exports{Slot: slot, Vars: []Var{
		{config.VarSlot, strconv.Itoa(slot)},
		{config.VarCopy, copyName},
		{config.VarCopySlug, Slug(copyName)},
		{config.VarMeta, metaRoot},
	}}
	if policy, ok := cfg.PortPolicy(); ok {
		for _, p := range policy.Ports {
			port := strconv.Itoa(p.Base + slot*policy.Stride)
			e.Vars = append(e.Vars, Var{p.VarName(), port})
			if p.Var != "" {
				e.Vars = append(e.Vars, Var{p.Var, port})
			}
		}
	}
	// Vars reference only ports and builtins, so rendering each against the
	// set built so far is order-independent.
	base := Exports{Slot: slot, Vars: slices.Clone(e.Vars)}
	for _, name := range slices.Sorted(maps.Keys(cfg.Vars)) {
		value, _ := Render([]byte(cfg.Vars[name]), base)
		e.Vars = append(e.Vars, Var{name, string(value)})
	}
	return e
}

// Slug derives MWS_COPY_SLUG from a copy name: lower-cased, with every byte
// outside [a-z0-9-] replaced by '-', so it is usable as a docker compose or
// Supabase project name.
func Slug(copyName string) string {
	lower := []byte(strings.ToLower(copyName))
	for i, b := range lower {
		if (b < 'a' || b > 'z') && (b < '0' || b > '9') && b != '-' {
			lower[i] = '-'
		}
	}
	return string(lower)
}

// Environ returns the export set as NAME=value entries, in order, for a
// child process environment.
func (e Exports) Environ() []string {
	env := make([]string, len(e.Vars))
	for i, v := range e.Vars {
		env[i] = v.Name + "=" + v.Value
	}
	return env
}

// Lookup returns the value of the exported variable name.
func (e Exports) Lookup(name string) (string, bool) {
	for _, v := range e.Vars {
		if v.Name == name {
			return v.Value, true
		}
	}
	return "", false
}
