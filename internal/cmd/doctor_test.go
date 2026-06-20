package cmd

import (
	"testing"

	"github.com/jinmu/eme/internal/git"
)

func TestKindLabel(t *testing.T) {
	cases := map[git.Kind]string{
		git.KindGreenfield: "empty / greenfield",
		git.KindNormalRoot: "normal git repo (adoptable in place)",
		git.KindSubmodule:  "git submodule (not adoptable)",
	}
	for k, want := range cases {
		if got := kindLabel(k); got != want {
			t.Errorf("kindLabel(%v) = %q, want %q", k, got, want)
		}
	}
}
