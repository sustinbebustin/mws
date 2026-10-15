package config

import (
	"fmt"
	"regexp"
	"slices"
)

// placeholder matches a ${NAME} reference. Any other '$' sequence is a
// literal, as in rendering (ADR-0015).
var placeholder = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// validateVars checks `[vars]`: each key is an identifier that no port or
// builtin already exports, and each value references only names in exported
// (port variables, aliases and builtins), never another var, since TOML
// tables are unordered and evaluation order would be undefined.
func validateVars(vars map[string]string, exported map[string]string) error {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		key := "vars." + k
		if !isIdentifier(k) {
			return fmt.Errorf("%s: %q is not a valid name; %s", key, k, identifierRule)
		}
		if prev, taken := exported[k]; taken {
			return fmt.Errorf("%s is already exported by %s; every exported variable needs its own name", key, prev)
		}
	}
	for _, k := range keys {
		for _, m := range placeholder.FindAllStringSubmatch(vars[k], -1) {
			ref := m[1]
			if _, ok := exported[ref]; ok {
				continue
			}
			if _, isVar := vars[ref]; isVar {
				return fmt.Errorf("vars.%s references ${%s}, another [vars] entry; a var may reference only port variables and builtins, so inline its value instead", k, ref)
			}
			return fmt.Errorf("vars.%s references ${%s}, which nothing exports; a var may reference port variables (MWS_PORT_<NAME> or a port's var alias) and the builtins %v", k, ref, Builtins)
		}
	}
	return nil
}
