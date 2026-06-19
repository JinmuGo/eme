package session

import (
	"path/filepath"
	"testing"
)

func TestID_UniqueForSameBasename(t *testing.T) {
	// Two folders with the same basename but different parents must produce
	// different session ids.
	a := filepath.Join("/home", "user", "projects", "foo")
	b := filepath.Join("/home", "user", "work", "foo")
	if ID(a) == ID(b) {
		t.Errorf("expected different ids for %q and %q, got %q", a, b, ID(a))
	}
}

func TestID_Stable(t *testing.T) {
	a := "/home/user/projects/foo"
	b := "/home/user/projects/foo"
	if ID(a) != ID(b) {
		t.Errorf("expected same id for repeated path, got %q and %q", ID(a), ID(b))
	}
}

func TestDisplayName(t *testing.T) {
	got := DisplayName("/home/user/projects/foo")
	if got != "foo" {
		t.Errorf("expected display name 'foo', got %q", got)
	}
}

func TestTmuxName(t *testing.T) {
	cases := map[string]string{
		"foo":         "foo",
		"Programming": "programming",
		"My Project":  "my-project",
	}
	for in, want := range cases {
		if got := TmuxName(in); got != want {
			t.Errorf("TmuxName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUniqueTmuxName_NoCollision(t *testing.T) {
	got := UniqueTmuxName("Programming", func(string) bool { return false })
	if got != "programming" {
		t.Errorf("expected clean name 'programming', got %q", got)
	}
}

func TestUniqueTmuxName_SuffixesOnCollision(t *testing.T) {
	taken := map[string]bool{"api": true, "api-2": true}
	got := UniqueTmuxName("api", func(name string) bool { return taken[name] })
	if got != "api-3" {
		t.Errorf("expected 'api-3' when api and api-2 are taken, got %q", got)
	}
}

func TestUniqueTmuxName_FirstCollision(t *testing.T) {
	taken := map[string]bool{"api": true}
	got := UniqueTmuxName("api", func(name string) bool { return taken[name] })
	if got != "api-2" {
		t.Errorf("expected 'api-2' when api is taken, got %q", got)
	}
}
