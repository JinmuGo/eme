package cmd

import (
	"errors"
	"testing"

	emeerrors "github.com/jinmu/eme/internal/errors"
	"github.com/jinmu/eme/internal/state"
)

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
