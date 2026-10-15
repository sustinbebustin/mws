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
// namespace, port range, stride, slots, and the collision rule.
func (p *Ports) validate() error {
	if p.Stride != nil && *p.Stride < 1 {
		return fmt.Errorf("ports.stride is %d; must be at least 1 (default %d)", *p.Stride, DefaultPortStride)
	}
	if p.Slots != nil && *p.Slots < 1 {
		return fmt.Errorf("ports.slots is %d; must be at least 1, or omitted for the largest collision-free count", *p.Slots)
	}
	if len(p.Port) == 0 {
		return fmt.Errorf("[ports] declares no ports; add at least one [[ports.port]] entry with a name and base, or remove the [ports] table")
	}
	owner := map[string]string{}
	for _, b := range Builtins {
		owner[b] = "the builtin " + b
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
	return p.validateCollisions()
}

// validateCollisions enforces exact slot arithmetic (ADR-0015): ports a < b
// with bases congruent modulo the stride collide when one copy holds slot
// d = (b-a)/stride and another sits on slot 0, so the highest slot must stay
// below d. Only an explicit ports.slots can promise that.
func (p *Ports) validateCollisions() error {
	stride := p.stride()
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
			if p.Slots != nil && *p.Slots < d {
				continue
			}
			pair := fmt.Sprintf("ports.port[%d].base (%d, %q) and ports.port[%d].base (%d, %q)",
				i, p.Port[i].Base, p.Port[i].Name, j, p.Port[j].Base, p.Port[j].Name)
			switch {
			case d == 0:
				return fmt.Errorf("%s are equal; every port needs its own base", pair)
			case d == 1:
				return fmt.Errorf("%s are one stride (%d) apart, so slot 1 of %q is slot 0 of %q; change a base so they differ modulo %d", pair, stride, p.Port[lo].Name, p.Port[hi].Name, stride)
			case p.Slots != nil:
				return fmt.Errorf("%s are congruent modulo stride %d, so slot %d of %q is slot 0 of %q, and ports.slots is %d; lower ports.slots to at most %d or change a base so they differ modulo %d", pair, stride, d, p.Port[lo].Name, p.Port[hi].Name, *p.Slots, d-1, stride)
			default:
				return fmt.Errorf("%s are congruent modulo stride %d, so slot %d of %q is slot 0 of %q; set ports.slots to at most %d or change a base so they differ modulo %d", pair, stride, d, p.Port[lo].Name, p.Port[hi].Name, d-1, stride)
			}
		}
	}
	return nil
}

// stride is the resolved stride, DefaultPortStride when absent.
func (p *Ports) stride() int {
	if p.Stride == nil {
		return DefaultPortStride
	}
	return *p.Stride
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

// Render maps a `.render` source under the harness dir to a target file at
// the working copy root.
type Render struct {
	Source string `toml:"source"`
	Target string `toml:"target"`
}
