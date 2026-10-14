package clierr

import (
	"errors"
	"fmt"
	"testing"
)

func TestKindOfFindsWrappedErrors(t *testing.T) {
	err := fmt.Errorf("context: %w", Refusedf("blocked"))
	if got := KindOf(err); got != Refused {
		t.Fatalf("KindOf = %v, want Refused", got)
	}
	if got := KindOf(errors.New("plain")); got != Failure {
		t.Fatalf("KindOf(untagged) = %v, want Failure", got)
	}
}

func TestJoinKeepsTheMostSevereKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		errs []error
		want Kind
	}{
		{"one usage", []error{Usagef("u")}, Usage},
		{"usage twice", []error{Usagef("a"), Usagef("b")}, Usage},
		{"usage and failure", []error{Usagef("u"), Failuref("f")}, Failure},
		{"usage and untagged", []error{Usagef("u"), errors.New("io")}, Failure},
		{"refused and not found", []error{Refusedf("r"), NotFoundf("n")}, Failure},
		{"nils skipped", []error{nil, NotFoundf("n"), nil}, NotFound},
	} {
		if got := KindOf(Join(tc.errs...)); got != tc.want {
			t.Errorf("%s: Join kind = %v, want %v", tc.name, got, tc.want)
		}
	}
	if Join(nil, nil) != nil {
		t.Fatal("Join of nils should be nil")
	}
}

func TestExitCodesAndCodes(t *testing.T) {
	for k, want := range map[Kind]struct {
		exit int
		code string
	}{
		Failure:  {1, "failure"},
		Usage:    {2, "usage"},
		Refused:  {3, "refused"},
		NotFound: {4, "not_found"},
	} {
		if k.ExitCode() != want.exit || k.Code() != want.code {
			t.Errorf("%v: exit %d code %q, want %d %q", k, k.ExitCode(), k.Code(), want.exit, want.code)
		}
	}
}
