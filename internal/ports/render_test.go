package ports

import (
	"bytes"
	"slices"
	"testing"
)

func TestRenderSubstitutesExportedNames(t *testing.T) {
	e := Exports{Slot: 2, Vars: []Var{{"MWS_SLOT", "2"}, {"MWS_PORT_API", "54341"}}}
	out, unresolved := Render([]byte("API=http://localhost:${MWS_PORT_API}/v1\nSLOT=${MWS_SLOT}${MWS_SLOT}\n"), e)
	if got, want := string(out), "API=http://localhost:54341/v1\nSLOT=22\n"; got != want {
		t.Fatalf("Render = %q, want %q", got, want)
	}
	if len(unresolved) != 0 {
		t.Fatalf("unresolved = %v, want none", unresolved)
	}
}

func TestRenderPassesThroughEveryOtherDollar(t *testing.T) {
	e := Exports{Vars: []Var{{"MWS_SLOT", "0"}}}
	src := "export PATH=$PATH:$HOME/bin\n${lower} ${Mixed} $${ ${ } ${1X} $MWS_SLOT ${MWS_SLOT\n"
	out, unresolved := Render([]byte(src), e)
	if string(out) != src {
		t.Fatalf("Render changed bytes:\n got %q\nwant %q", out, src)
	}
	if len(unresolved) != 0 {
		t.Fatalf("unresolved = %v, want none", unresolved)
	}
}

func TestRenderReportsUnsubstitutedUpperCaseNames(t *testing.T) {
	e := Exports{Vars: []Var{{"MWS_SLOT", "0"}}}
	src := "A=${MWS_PORT_TYPO}\nB=${OTHER_1}\nC=${MWS_PORT_TYPO}\nD=${lower}\nE=${MWS_SLOT}\n"
	out, unresolved := Render([]byte(src), e)
	if want := "A=${MWS_PORT_TYPO}\nB=${OTHER_1}\nC=${MWS_PORT_TYPO}\nD=${lower}\nE=0\n"; string(out) != want {
		t.Fatalf("Render = %q, want %q", out, want)
	}
	if want := []string{"MWS_PORT_TYPO", "OTHER_1"}; !slices.Equal(unresolved, want) {
		t.Fatalf("unresolved = %v, want %v", unresolved, want)
	}
}

func TestRenderWithoutPlaceholdersIsByteForByte(t *testing.T) {
	src := []byte("KEY=value\r\n\x00\xff$\n")
	out, _ := Render(src, Exports{})
	if !bytes.Equal(out, src) {
		t.Fatalf("Render = %q, want %q", out, src)
	}
}
