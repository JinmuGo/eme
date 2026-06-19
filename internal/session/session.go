// Package session generates stable, unique identifiers for eme sessions.
package session

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
)

// ID returns a stable session id for a folder root.
// It combines the absolute folder path with a hash so that two folders with
// the same basename never collide.
func ID(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	h := sha256.Sum256([]byte(abs))
	short := fmt.Sprintf("%x", h)[:8]
	base := slug(filepath.Base(abs))
	return fmt.Sprintf("%s-%s", base, short)
}

// DisplayName returns the human-readable name for a session.
func DisplayName(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	return filepath.Base(abs)
}

// TmuxName returns the tmux session name for a display name, e.g.
// "Programming" -> "programming". eme sessions are plain tmux sessions with no
// prefix; use UniqueTmuxName to resolve collisions between folders that share a
// basename (or with existing non-eme sessions).
func TmuxName(displayName string) string {
	return slug(displayName)
}

// UniqueTmuxName returns a tmux session name for displayName that does not
// collide. It starts from TmuxName(displayName) and appends "-2", "-3", ...
// until taken reports the name as free. taken should report names already used
// by live tmux sessions or other eme sessions.
func UniqueTmuxName(displayName string, taken func(name string) bool) string {
	base := TmuxName(displayName)
	if !taken(base) {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !taken(candidate) {
			return candidate
		}
	}
}

func slug(name string) string {
	name = strings.ToLower(name)
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
}
