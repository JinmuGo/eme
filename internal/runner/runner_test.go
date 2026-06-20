// internal/runner/runner_test.go
package runner

import (
	"context"
	"testing"
)

func TestMock_RunEnv_RecordsEnvAndMatchesKey(t *testing.T) {
	m := NewMock()
	m.Set("git", []string{"rev-parse"}, "ok", "", nil)

	out, _, err := m.RunEnv(context.Background(), []string{"PATH=/bin"}, "git", "rev-parse")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "ok" {
		t.Errorf("stdout = %q, want ok", out)
	}
	if len(m.Calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(m.Calls))
	}
	if got := m.Calls[0].Env; len(got) != 1 || got[0] != "PATH=/bin" {
		t.Errorf("recorded env = %v, want [PATH=/bin]", got)
	}
}
