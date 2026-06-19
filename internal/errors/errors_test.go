package errors

import (
	"fmt"
	"strings"
	"testing"
)

// The "eme:" prefix is a CLI presentation concern owned by the entry point
// (cmd/eme/main.go). The error value itself must not embed it, otherwise the
// printed line doubles up as "eme: eme: ...".
func TestError_NoEmePrefix(t *testing.T) {
	e := New("test_code", "something went wrong", "because reasons", "do this")
	got := e.Error()

	if strings.HasPrefix(got, "eme:") {
		t.Errorf("Error() must not embed the 'eme:' prefix, got: %q", got)
	}
	want := "something went wrong\n  Cause: because reasons\n  Fix: do this"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestError_IncludesDetailsWhenWrapped(t *testing.T) {
	underlying := fmt.Errorf("exit status 1")
	e := Wrap("cmd_failed", "command failed", "git blew up", "try --verbose", underlying)
	got := e.Error()

	if strings.HasPrefix(got, "eme:") {
		t.Errorf("Error() must not embed the 'eme:' prefix, got: %q", got)
	}
	for _, want := range []string{
		"command failed",
		"\n  Cause: git blew up",
		"\n  Fix: try --verbose",
		"\n  Details: exit status 1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() missing %q, got: %q", want, got)
		}
	}
}
