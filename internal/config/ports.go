package config

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

// Render maps a `.render` source under the harness dir to a target file at
// the working copy root.
type Render struct {
	Source string `toml:"source"`
	Target string `toml:"target"`
}
