package cmd

import (
	"errors"
	"testing"

	"github.com/alderwork/eme/internal/config"
	emeerrors "github.com/alderwork/eme/internal/errors"
	"github.com/alderwork/eme/internal/state"
)

// TestResolveTmuxSocket_Precedence locks the order EME_TMUX_SOCKET > config,
// with an empty result meaning ambient mode (no pin).
func TestResolveTmuxSocket_Precedence(t *testing.T) {
	t.Run("ambient when config empty and no env", func(t *testing.T) {
		t.Setenv("EME_TMUX_SOCKET", "")
		if got := resolveTmuxSocket(&config.Config{}); got != "" {
			t.Fatalf("got %q, want \"\" (ambient)", got)
		}
	})
	t.Run("config pins a socket", func(t *testing.T) {
		t.Setenv("EME_TMUX_SOCKET", "")
		cfg := &config.Config{Tmux: config.Tmux{Socket: "work"}}
		if got := resolveTmuxSocket(cfg); got != "work" {
			t.Fatalf("got %q, want %q", got, "work")
		}
	})
	t.Run("env overrides config", func(t *testing.T) {
		t.Setenv("EME_TMUX_SOCKET", "envsock")
		cfg := &config.Config{Tmux: config.Tmux{Socket: "work"}}
		if got := resolveTmuxSocket(cfg); got != "envsock" {
			t.Fatalf("got %q, want %q", got, "envsock")
		}
	})
}

func TestResolveSession_ByID(t *testing.T) {
	s := &state.State{Sessions: []state.Session{
		{ID: "foo-1234abcd", DisplayName: "foo", Root: "/tmp/foo"},
	}}
	got, err := resolveSession(s, "foo-1234abcd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "foo-1234abcd" {
		t.Errorf("expected foo-1234abcd, got %q", got.ID)
	}
}

func TestResolveSession_ByDisplayName(t *testing.T) {
	s := &state.State{Sessions: []state.Session{
		{ID: "foo-1234abcd", DisplayName: "foo", Root: "/tmp/foo"},
	}}
	got, err := resolveSession(s, "foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "foo-1234abcd" {
		t.Errorf("expected foo-1234abcd, got %q", got.ID)
	}
}

func TestResolveSession_AmbiguousDisplayName(t *testing.T) {
	s := &state.State{Sessions: []state.Session{
		{ID: "foo-a1b2c3d4", DisplayName: "foo", Root: "/tmp/a/foo"},
		{ID: "foo-e5f6g7h8", DisplayName: "foo", Root: "/tmp/b/foo"},
	}}
	_, err := resolveSession(s, "foo")
	if err == nil {
		t.Fatal("expected error for ambiguous name")
	}
	var emeErr *emeerrors.EmeError
	if !errors.As(err, &emeErr) {
		t.Errorf("expected *EmeError, got %T", err)
	}
}

func TestResolveSession_NotFound(t *testing.T) {
	s := &state.State{Sessions: []state.Session{}}
	_, err := resolveSession(s, "missing")
	if err == nil {
		t.Fatal("expected error for missing session")
	}
}
