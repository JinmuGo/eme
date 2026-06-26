package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/muesli/termenv"

	"github.com/JinmuGo/eme/internal/gh"
	"github.com/JinmuGo/eme/internal/git"
)

func TestCheckGh(t *testing.T) {
	prevLook := gh.LookPath
	prevRunner := gh.Runner
	t.Cleanup(func() { gh.LookPath = prevLook; gh.Runner = prevRunner })

	gh.LookPath = func(string) (string, error) { return "", errors.New("missing") }
	if ok, msg := checkGh(); ok || !strings.Contains(msg, "not installed") {
		t.Errorf("missing gh: ok=%v msg=%q", ok, msg)
	}
}

func TestColorProfileMessage(t *testing.T) {
	if msg := colorProfileMessage(termenv.TrueColor, true); !strings.Contains(msg, "truecolor") {
		t.Errorf("truecolor profile = %q, want it to mention truecolor", msg)
	}
	// 256-color inside tmux is the actionable case: it must surface the terminal-features fix.
	inTmux := colorProfileMessage(termenv.ANSI256, true)
	if !strings.Contains(inTmux, "256-color") || !strings.Contains(inTmux, "terminal-features") {
		t.Errorf("256-color in tmux = %q, want 256-color + the terminal-features tip", inTmux)
	}
	// Outside tmux the tip would be noise, so it must NOT appear.
	if outTmux := colorProfileMessage(termenv.ANSI256, false); strings.Contains(outTmux, "terminal-features") {
		t.Errorf("256-color outside tmux = %q, should not mention terminal-features", outTmux)
	}
}

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
