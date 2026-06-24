package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// DefaultPickerMaxDepth is how many levels deep the folder picker scans by default.
const DefaultPickerMaxDepth = 3

// Config holds user configuration for eme.
type Config struct {
	Agent    Agent       `toml:"agent"`
	Picker   Picker      `toml:"picker"`
	Worktree Worktree    `toml:"worktree"`
	Tmux     Tmux        `toml:"tmux"`
	Status   Status      `toml:"status"`
	Agents   []AgentSpec `toml:"agents"`
}

// Status configures the agent-status signals.
type Status struct {
	// QuietAfter is how long a hooked agent may sit in "working" before the dashboard
	// dims it as gone-quiet. A Go duration; "" → default 2m; "0"/"0s" → disabled.
	QuietAfter string `toml:"quiet_after"`
}

// QuietAfterDuration parses Status.QuietAfter. Empty/invalid → 2m; "0"/"0s" → 0 (disabled).
func (c *Config) QuietAfterDuration() time.Duration {
	s := strings.TrimSpace(c.Status.QuietAfter)
	if s == "" {
		return 2 * time.Minute
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 2 * time.Minute
	}
	if d < 0 {
		return 0
	}
	return d
}

// Tmux configures how eme talks to tmux.
type Tmux struct {
	// Socket optionally pins every tmux operation to one server (tmux -L
	// <socket>). The default is empty: eme is "native to your tmux" and operates
	// on whatever server you are currently attached to (the popup's host, or the
	// default socket when outside tmux), so switching to a worktree moves your
	// real client and the popup closes. Set a socket name only if you want a
	// single dedicated eme server shared across every launch context — note that
	// switching from a popup hosted on a *different* server then attaches into the
	// popup instead of moving your client (a tmux client cannot cross servers).
	// The EME_TMUX_SOCKET env var overrides this value.
	Socket string `toml:"socket"`
}

// Agent configures agent execution.
type Agent struct {
	Command string `toml:"command"`
}

// AgentSpec is one launchable agent in the catalog. Command is the shell line
// eme types into the worktree pane; it runs with the worktree as its cwd.
type AgentSpec struct {
	Name    string `toml:"name"`
	Command string `toml:"command"`
}

// BuiltinAgents is the catalog eme ships out of the box.
func BuiltinAgents() []AgentSpec {
	return []AgentSpec{
		{Name: "claude", Command: "claude"},
		{Name: "codex", Command: "codex"},
		{Name: "gemini", Command: "gemini"},
		{Name: "opencode", Command: "opencode"},
	}
}

// Catalog returns the merged agent catalog: builtins first, with user [[agents]]
// overriding a builtin's command when names match and appending otherwise. A
// custom legacy agent.command that is not already represented is surfaced as a
// trailing entry so existing setups still appear and can be the default.
func (c *Config) Catalog() []AgentSpec {
	out := append([]AgentSpec(nil), BuiltinAgents()...)
	for _, u := range c.Agents {
		if u.Name == "" || u.Command == "" {
			continue
		}
		idx := -1
		for i := range out {
			if out[i].Name == u.Name {
				idx = i
				break
			}
		}
		if idx >= 0 {
			out[idx].Command = u.Command
		} else {
			out = append(out, u)
		}
	}
	if c.Agent.Command != "" {
		found := false
		for _, a := range out {
			if a.Command == c.Agent.Command || a.Name == c.Agent.Command {
				found = true
				break
			}
		}
		if !found {
			fields := strings.Fields(c.Agent.Command)
			name := c.Agent.Command
			if len(fields) > 0 {
				name = filepath.Base(fields[0])
			}
			out = append(out, AgentSpec{Name: name, Command: c.Agent.Command})
		}
	}
	return out
}

// Picker configures the folder picker scan.
type Picker struct {
	// MaxDepth is how many directory levels deep to scan from each root.
	MaxDepth int `toml:"max_depth"`
	// Roots are extra directories to scan in addition to the auto-discovered
	// ones. A leading "~" is expanded to the user's home directory.
	Roots []string `toml:"roots"`
}

// Worktree configures where in-place worktrees are created.
type Worktree struct {
	// DirTemplate is the sibling directory name for an adopted repo's worktrees.
	// {repo} expands to the repo basename. Must resolve to a sibling of root.
	DirTemplate string `toml:"dir_template"`
}

// Default returns a config with sensible defaults.
func Default() *Config {
	return &Config{
		Agent:    Agent{Command: "opencode"},
		Picker:   Picker{MaxDepth: DefaultPickerMaxDepth},
		Worktree: Worktree{DirTemplate: "{repo}.worktrees"},
		Status:   Status{QuietAfter: "2m"},
		// Tmux.Socket defaults to "" (ambient: use your current tmux server).
	}
}

// DefaultPath returns the default config file path.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	return filepath.Join(home, ".config", "eme", "config.toml")
}

// Load reads config from path, returning defaults if the file does not exist.
func Load(path string) (*Config, error) {
	cfg := Default()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if cfg.Agent.Command == "" {
		cfg.Agent.Command = "opencode"
	}
	if cfg.Picker.MaxDepth <= 0 {
		cfg.Picker.MaxDepth = DefaultPickerMaxDepth
	}
	if cfg.Worktree.DirTemplate == "" {
		cfg.Worktree.DirTemplate = "{repo}.worktrees"
	}
	// cfg.Tmux.Socket is left as-is: "" means ambient (use the current server).
	return cfg, nil
}

// Save writes the config file with default content if missing.
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open config: %w", err)
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	return enc.Encode(c)
}

// WorktreeDirFor resolves the worktree container directory for an in-place root.
// The template must produce a sibling of root (no absolute path, no parent escape).
func WorktreeDirFor(template, root string) (string, error) {
	name := strings.ReplaceAll(template, "{repo}", filepath.Base(root))
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("worktree dir_template must be relative, got %q", template)
	}
	resolved := filepath.Join(filepath.Dir(root), name)
	parent := filepath.Dir(root)
	if rel, err := filepath.Rel(parent, resolved); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("worktree dir_template must stay within the project's parent dir, got %q", template)
	}
	return resolved, nil
}
