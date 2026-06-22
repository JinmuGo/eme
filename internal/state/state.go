// Package state persists eme metadata and reconciles it with live tmux/git state.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// Version is the current state file schema version.
const Version = 1

const (
	LayoutNestedBare = "nested-bare"
	LayoutInPlace    = "in-place"
	// LayoutPlain is a folder eme runs an agent in directly, with no git
	// management: the root is the single "main" worktree, there is no .bare and
	// no nested main/, and worktree creation is unavailable (no git). Used to
	// adopt a non-git folder (e.g. a multi-repo parent dir) in place.
	LayoutPlain = "plain"
)

// Worktree represents one linked worktree inside a project.
type Worktree struct {
	Name                 string `json:"name"`
	Branch               string `json:"branch"`
	Path                 string `json:"path"`
	TmuxWindowID         string `json:"tmux_window_id"`
	AgentPID             int    `json:"agent_pid,omitempty"`
	AgentCommandOverride string `json:"agent_command_override,omitempty"`
	LastAgentCommand     string `json:"last_agent_command,omitempty"`
}

// Session represents a project folder mapped to a tmux session.
type Session struct {
	ID                  string     `json:"id"`
	DisplayName         string     `json:"display_name"`
	Root                string     `json:"root"`
	TmuxName            string     `json:"tmux_name"`
	AgentCommand        string     `json:"agent_command,omitempty"`
	Worktrees           []Worktree `json:"worktrees"`
	Layout              string     `json:"layout,omitempty"`
	WorktreeDirOverride string     `json:"worktree_dir,omitempty"`
}

// State is the persisted runtime state for eme.
type State struct {
	Version  int       `json:"version"`
	Sessions []Session `json:"sessions"`
}

// DefaultPath returns the default state file path.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	return filepath.Join(home, ".local", "share", "eme", "state.json")
}

// DefaultLockPath returns the default lock file path.
func DefaultLockPath() string {
	return DefaultPath() + ".lock"
}

// Load reads state from path, returning an empty state if the file does not exist.
func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{Version: Version}, nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	if s.Version != Version {
		return nil, fmt.Errorf("state file version %d is not supported by this version of eme (expected %d). Please delete or migrate %s", s.Version, Version, path)
	}
	for i := range s.Sessions {
		if s.Sessions[i].Layout == "" {
			s.Sessions[i].Layout = LayoutNestedBare
		}
	}
	return &s, nil
}

// Save writes state to path atomically, protected by a file lock.
func (s *State) Save(path string) error {
	s.Version = Version

	// Ensure the parent directory exists before creating the lock file.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	lockPath := path + ".lock"
	lock := flock.New(lockPath)
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("lock state: %w", err)
	}
	defer lock.Unlock()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write state temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename state: %w", err)
	}
	return nil
}

// SessionByID returns the session with the given id, or nil.
func (s *State) SessionByID(id string) *Session {
	for i := range s.Sessions {
		if s.Sessions[i].ID == id {
			return &s.Sessions[i]
		}
	}
	return nil
}

// SessionByRoot returns the session for the given root path, or nil.
func (s *State) SessionByRoot(root string) *Session {
	for i := range s.Sessions {
		if s.Sessions[i].Root == root {
			return &s.Sessions[i]
		}
	}
	return nil
}

// WorktreeByName returns the worktree for a session with the given name, or nil.
func (s *Session) WorktreeByName(name string) *Worktree {
	for i := range s.Worktrees {
		if s.Worktrees[i].Name == name {
			return &s.Worktrees[i]
		}
	}
	return nil
}

// MainPath returns the working directory of the project's main worktree.
func (s *Session) MainPath() string {
	if s.Layout == LayoutInPlace || s.Layout == LayoutPlain {
		return s.Root
	}
	return filepath.Join(s.Root, "main")
}

// GitDir returns the canonical git directory for the project.
// It is part of the derived-path API (alongside MainPath and WorktreeDir);
// retained for completeness and git-dir-targeted operations even though no
// caller reads it today.
func (s *Session) GitDir() string {
	if s.Layout == LayoutInPlace {
		return filepath.Join(s.Root, ".git")
	}
	return filepath.Join(s.Root, ".bare")
}

// WorktreeDir returns the parent directory under which new worktrees are created.
func (s *Session) WorktreeDir() string {
	if s.Layout == LayoutInPlace {
		if s.WorktreeDirOverride != "" {
			return s.WorktreeDirOverride
		}
		return s.Root + ".worktrees"
	}
	return s.Root
}

// AddSession appends a session.
func (s *State) AddSession(sess Session) {
	s.Sessions = append(s.Sessions, sess)
}

// RemoveSession removes a session and all its worktrees.
func (s *State) RemoveSession(id string) {
	sessions := s.Sessions[:0]
	for _, sess := range s.Sessions {
		if sess.ID != id {
			sessions = append(sessions, sess)
		}
	}
	s.Sessions = sessions
}

// AddWorktree appends a worktree to a session.
func (s *Session) AddWorktree(w Worktree) {
	s.Worktrees = append(s.Worktrees, w)
}

// RemoveWorktree removes a worktree by name from a session.
func (s *Session) RemoveWorktree(name string) {
	worktrees := s.Worktrees[:0]
	for _, w := range s.Worktrees {
		if w.Name != name {
			worktrees = append(worktrees, w)
		}
	}
	s.Worktrees = worktrees
}
