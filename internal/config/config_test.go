package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCatalog_IncludesBuiltins(t *testing.T) {
	c := Default() // Agent.Command == "opencode"
	got := c.Catalog()
	names := map[string]string{}
	for _, a := range got {
		names[a.Name] = a.Command
	}
	for _, want := range []string{"claude", "codex", "gemini", "opencode"} {
		if names[want] != want {
			t.Errorf("catalog missing builtin %q (command=%q); got %v", want, names[want], names)
		}
	}
}

func TestCatalog_UserOverridesBuiltinCommandByName(t *testing.T) {
	c := Default()
	c.Agents = []AgentSpec{{Name: "claude", Command: "claude --resume"}}
	found := false
	for _, a := range c.Catalog() {
		if a.Name == "claude" {
			found = true
			if a.Command != "claude --resume" {
				t.Errorf("claude command = %q, want %q", a.Command, "claude --resume")
			}
		}
	}
	if !found {
		t.Errorf("builtin 'claude' missing from catalog after override")
	}
}

func TestCatalog_AppendsCustomAgent(t *testing.T) {
	c := Default()
	c.Agents = []AgentSpec{{Name: "aider", Command: "aider"}}
	found := false
	for _, a := range c.Catalog() {
		if a.Name == "aider" && a.Command == "aider" {
			found = true
		}
	}
	if !found {
		t.Errorf("custom agent 'aider' not in catalog: %v", c.Catalog())
	}
}

func TestCatalog_SurfacesCustomLegacyCommand(t *testing.T) {
	c := Default()
	c.Agent.Command = "my-agent --flag" // not a builtin
	found := false
	for _, a := range c.Catalog() {
		if a.Command == "my-agent --flag" {
			found = true
		}
	}
	if !found {
		t.Errorf("legacy agent.command not surfaced in catalog: %v", c.Catalog())
	}
}

func TestWorktreeDirFor_DefaultSibling(t *testing.T) {
	got, err := WorktreeDirFor("{repo}.worktrees", "/p/myapp")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "/p/myapp.worktrees" {
		t.Errorf("got %q", got)
	}
}

func TestWorktreeDirFor_RejectsAbsolute(t *testing.T) {
	if _, err := WorktreeDirFor("/abs/{repo}", "/p/myapp"); err == nil {
		t.Errorf("expected rejection of absolute template")
	}
}

func TestWorktreeDirFor_RejectsParentEscape(t *testing.T) {
	if _, err := WorktreeDirFor("../{repo}.wt", "/p/myapp"); err == nil {
		t.Errorf("expected rejection of parent-escaping template")
	}
}

func TestDefault_WorktreeTemplate(t *testing.T) {
	if Default().Worktree.DirTemplate != "{repo}.worktrees" {
		t.Errorf("default template = %q", Default().Worktree.DirTemplate)
	}
}

func TestDefault_TmuxSocket_IsAmbient(t *testing.T) {
	if got := Default().Tmux.Socket; got != "" {
		t.Errorf("default tmux socket = %q, want \"\" (ambient)", got)
	}
}

func TestQuietAfterDuration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"", 2 * time.Minute},
		{"90s", 90 * time.Second},
		{"5m", 5 * time.Minute},
		{"garbage", 2 * time.Minute},
		{"0", 0},
		{"0s", 0},
	}
	for _, c := range cases {
		got := (&Config{Status: Status{QuietAfter: c.in}}).QuietAfterDuration()
		if got != c.want {
			t.Errorf("QuietAfterDuration(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestLoad_PreservesConfiguredSocket checks that an explicit [tmux] socket is
// honored, and that omitting it leaves ambient mode ("") rather than forcing a
// pinned server.
func TestLoad_PreservesConfiguredSocket(t *testing.T) {
	dir := t.TempDir()

	ambient := filepath.Join(dir, "ambient.toml")
	if err := os.WriteFile(ambient, []byte("[agent]\ncommand = \"claude\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if cfg, err := Load(ambient); err != nil {
		t.Fatalf("Load: %v", err)
	} else if cfg.Tmux.Socket != "" {
		t.Errorf("omitted socket = %q, want \"\" (ambient)", cfg.Tmux.Socket)
	}

	pinned := filepath.Join(dir, "pinned.toml")
	if err := os.WriteFile(pinned, []byte("[tmux]\nsocket = \"eme\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if cfg, err := Load(pinned); err != nil {
		t.Fatalf("Load: %v", err)
	} else if cfg.Tmux.Socket != "eme" {
		t.Errorf("configured socket = %q, want %q", cfg.Tmux.Socket, "eme")
	}
}
