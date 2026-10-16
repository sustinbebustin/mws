package ports

import (
	"regexp"
	"slices"
)

var (
	// placeholder matches a ${NAME} reference. Every other '$' sequence,
	// bare $NAME included, is literal (ADR-0015).
	placeholder = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	// upperCase is the shape of a name a Render reports when nothing exports
	// it; ${lower} is left alone silently, as shell and dotenv-expand
	// placeholders often are.
	upperCase = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
)

// Render substitutes ${NAME} for every NAME in e and passes every other byte
// of src through. unresolved lists, in order of first appearance, the
// ${UPPER_CASE} names left in place because e does not export them, so the
// caller can warn about a likely typo. Render is a pure function of
// (src, e); it never reads a previous output.
func Render(src []byte, e Exports) (out []byte, unresolved []string) {
	out = placeholder.ReplaceAllFunc(src, func(m []byte) []byte {
		name := string(m[2 : len(m)-1])
		if v, ok := e.Lookup(name); ok {
			return []byte(v)
		}
		if upperCase.MatchString(name) && !slices.Contains(unresolved, name) {
			unresolved = append(unresolved, name)
		}
		return m
	})
	return out, unresolved
}
