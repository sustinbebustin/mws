package config

import (
	"fmt"
	"strings"
)

// Ports is the raw on-disk `[ports]` table. Pointer fields separate "absent"
// (take the default) from an explicit value; callers use Config.PortPolicy
// instead of reading these fields.
type Ports struct {
	// Stride is how far slot k moves every port: port = base + k*stride.
	Stride *int `toml:"stride,omitempty"`
	// Slots caps the highest claimable slot.
	Slots *int `toml:"slots,omitempty"`
	// Port lists the named ports, in declaration order.
	Port []Port `toml:"port,omitempty"`
}

// Port is one `[[ports.port]]` entry.
type Port struct {
	// Name is exported as MWS_PORT_<NAME>, upper-cased.
	Name string `toml:"name"`
	// Base is the port at slot 0, the default ports.
	Base int `toml:"base"`
	// Var is an optional alias exported alongside MWS_PORT_<NAME>.
	Var string `toml:"var,omitempty"`
}

// Port bounds and defaults.
const (
	// DefaultPortStride applies when `[ports]` omits stride.
	DefaultPortStride = 10
	// MaxPort is the highest valid TCP/UDP port.
	MaxPort = 65535
	// PortVarPrefix prefixes every port's exported variable.
	PortVarPrefix = "MWS_PORT_"
)

// Builtin variables exported in every port lease, usable in `[vars]` and in
// any rendered source.
const (
	VarSlot     = "MWS_SLOT"
	VarCopy     = "MWS_COPY"
	VarCopySlug = "MWS_COPY_SLUG"
	VarMeta     = "MWS_META"
)

// Builtins lists the builtin variable names.
var Builtins = []string{VarSlot, VarCopy, VarCopySlug, VarMeta}

// VarName is the port's canonical exported variable, MWS_PORT_<NAME>.
func (p Port) VarName() string {
	return PortVarPrefix + strings.ToUpper(p.Name)
}

const identifierRule = "must be an identifier (letters, digits, '_', not starting with a digit)"

// validate checks the `[ports]` table: identifiers, the exported variable
// namespace, port range, stride, slots, and the collision rule. owner maps
// each exported variable to the key that claims it; validate adds every
// port variable and alias to it.
func (p *Ports) validate(owner map[string]string) error {
	if p.Stride != nil && *p.Stride < 1 {
		return fmt.Errorf("ports.stride is %d; must be at least 1 (default %d)", *p.Stride, DefaultPortStride)
	}
	if p.Slots != nil && *p.Slots < 1 {
		return fmt.Errorf("ports.slots is %d; must be at least 1, or omitted for the largest collision-free count", *p.Slots)
	}
	if len(p.Port) == 0 {
		return fmt.Errorf("[ports] declares no ports; add at least one [[ports.port]] entry with a name and base, or remove the [ports] table")
	}
	for i, port := range p.Port {
		key := fmt.Sprintf("ports.port[%d].name", i)
		if !isIdentifier(port.Name) {
			return fmt.Errorf("%s is %q; %s", key, port.Name, identifierRule)
		}
		if prev, taken := owner[port.VarName()]; taken {
			return fmt.Errorf("%s is %q, which exports %s, already exported by %s; port names must be unique ignoring case", key, port.Name, port.VarName(), prev)
		}
		owner[port.VarName()] = key
		if port.Base < 1 || port.Base > MaxPort {
			return fmt.Errorf("ports.port[%d].base is %d; must be in 1..%d", i, port.Base, MaxPort)
		}
	}
	for i, port := range p.Port {
		if port.Var == "" {
			continue
		}
		key := fmt.Sprintf("ports.port[%d].var", i)
		if !isIdentifier(port.Var) {
			return fmt.Errorf("%s is %q; %s", key, port.Var, identifierRule)
		}
		if prev, taken := owner[port.Var]; taken {
			return fmt.Errorf("%s is %q, already exported by %s; every exported variable needs its own name", key, port.Var, prev)
		}
		owner[port.Var] = key
	}
	if err := p.validateLimit(); err != nil {
		return err
	}
	return p.validateCollisions()
}

// validateLimit requires at least slot 1 to be claimable: the highest base
// plus one stride must stay at or below MaxPort, or no copy could ever leave
// the default ports.
func (p *Ports) validateLimit() error {
	if p.limit() >= 1 {
		return nil
	}
	top := 0
	for i, port := range p.Port {
		if port.Base > p.Port[top].Base {
			top = i
		}
	}
	stride := p.stride()
	fix := fmt.Sprintf("lower that base to at most %d or lower ports.stride", MaxPort-stride)
	if MaxPort-stride < 1 {
		fix = fmt.Sprintf("lower ports.stride below %d", MaxPort)
	}
	return fmt.Errorf("ports.port[%d].base (%d, %q) is the highest base, and at stride %d its slot 1 would be port %d, above %d, so no slot is claimable; %s",
		top, p.Port[top].Base, p.Port[top].Name, stride, p.Port[top].Base+stride, MaxPort, fix)
}

// validateCollisions enforces exact slot arithmetic (ADR-0015): ports a < b
// with bases congruent modulo the stride collide when one copy holds slot
// d = (b-a)/stride and another sits on slot 0, so the slot limit must stay
// below d. The limit is the 65535 bound or a lower ports.slots cap.
func (p *Ports) validateCollisions() error {
	stride, limit := p.stride(), p.limit()
	for i := range p.Port {
		for j := i + 1; j < len(p.Port); j++ {
			lo, hi := i, j
			if p.Port[lo].Base > p.Port[hi].Base {
				lo, hi = hi, lo
			}
			gap := p.Port[hi].Base - p.Port[lo].Base
			if gap%stride != 0 {
				continue
			}
			d := gap / stride
			if limit < d {
				continue
			}
			pair := fmt.Sprintf("ports.port[%d].base (%d, %q) and ports.port[%d].base (%d, %q)",
				i, p.Port[i].Base, p.Port[i].Name, j, p.Port[j].Base, p.Port[j].Name)
			switch d {
			case 0:
				return fmt.Errorf("%s are equal; every port needs its own base", pair)
			case 1:
				return fmt.Errorf("%s are one stride (%d) apart, so slot 1 of %q is slot 0 of %q; change a base so they differ modulo %d", pair, stride, p.Port[lo].Name, p.Port[hi].Name, stride)
			default:
				return fmt.Errorf("%s are congruent modulo stride %d, so slot %d of %q is slot 0 of %q, but slots up to %d are claimable; set ports.slots to at most %d or change a base so they differ modulo %d", pair, stride, d, p.Port[lo].Name, p.Port[hi].Name, limit, d-1, stride)
			}
		}
	}
	return nil
}

// PortPolicy is the resolved `[ports]` table, with defaults and both slot
// bounds applied so no caller recomputes them.
type PortPolicy struct {
	// Stride is how far each slot moves every port.
	Stride int
	// Ports lists the named ports in declaration order.
	Ports []Port
	// Limit is the highest claimable slot; slots 1..Limit are valid and
	// slot 0 is the default ports. Validate guarantees no two ports collide
	// at or below it.
	Limit int
}

// PortPolicy resolves the effective port policy. ok is false when the config
// has no `[ports]` table, so the workspace has no port slots. The config
// must have passed Validate, as every loaded config has.
func (c *Config) PortPolicy() (policy PortPolicy, ok bool) {
	if c.Ports == nil {
		return PortPolicy{}, false
	}
	return PortPolicy{Stride: c.Ports.stride(), Ports: c.Ports.Port, Limit: c.Ports.limit()}, true
}

// stride is the resolved stride, DefaultPortStride when absent.
func (p *Ports) stride() int {
	if p.Stride == nil {
		return DefaultPortStride
	}
	return *p.Stride
}

// limit is the highest claimable slot: the last slot whose ports all stay at
// or below MaxPort, lowered to ports.slots when that is set.
func (p *Ports) limit() int {
	highest := 0
	for _, port := range p.Port {
		highest = max(highest, port.Base)
	}
	limit := (MaxPort - highest) / p.stride()
	if p.Slots != nil {
		limit = min(limit, *p.Slots)
	}
	return limit
}

// isIdentifier reports whether s is a shell-style variable name:
// [A-Za-z_][A-Za-z0-9_]*.
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
