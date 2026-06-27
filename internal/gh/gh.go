// Package gh is a thin, mockable wrapper over the GitHub CLI (`gh`). It isolates
// all gh-specific knowledge (argv, JSON shape, auth) from the rest of eme so
// `internal/git` and `internal/cmd` never shell out to gh directly.
package gh

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"

	"github.com/alderwork/eme/internal/runner"
)

// Runner runs external commands; tests replace it with runner.NewMock().
var Runner runner.Runner = runner.Default

// LookPath resolves a binary on PATH; tests replace it. origLookPath restores it.
var (
	origLookPath = exec.LookPath
	LookPath     = exec.LookPath
)

// Repo is one repository as returned by `gh repo list --json`.
type Repo struct {
	NameWithOwner string `json:"nameWithOwner"`
	Description   string `json:"description"`
	IsPrivate     bool   `json:"isPrivate"`
	UpdatedAt     string `json:"updatedAt"` // RFC3339; used for sort only
}

// Available reports whether the gh CLI is on PATH.
func Available() bool {
	_, err := LookPath("gh")
	return err == nil
}

// Authed reports whether gh's ACTIVE account is authenticated. It scopes the
// check to --active on purpose: plain `gh auth status` exits non-zero when any
// other stored account has an invalid token, which would wrongly report a working
// active account (a common case after renaming/re-adding an account) as logged out.
func Authed(ctx context.Context) bool {
	_, _, err := Runner.Run(ctx, "gh", "auth", "status", "--active")
	return err == nil
}

// RepoList returns the caller's repositories, most-recently-updated first.
func RepoList(ctx context.Context, limit int) ([]Repo, error) {
	out, _, err := Runner.Run(ctx, "gh", "repo", "list",
		"--limit", fmt.Sprintf("%d", limit),
		"--json", "nameWithOwner,description,isPrivate,updatedAt")
	if err != nil {
		return nil, fmt.Errorf("gh repo list: %w", err)
	}
	var repos []Repo
	if err := json.Unmarshal([]byte(out), &repos); err != nil {
		return nil, fmt.Errorf("decode gh repo list: %w", err)
	}
	sort.SliceStable(repos, func(i, j int) bool {
		return repos[i].UpdatedAt > repos[j].UpdatedAt // RFC3339 sorts lexically by time
	})
	return repos, nil
}

// CloneBare clones spec as a bare repository into destBare via gh (which supplies
// URL resolution and auth); the "--" forwards "--bare" to the underlying git clone.
func CloneBare(ctx context.Context, spec, destBare string) error {
	_, stderr, err := Runner.Run(ctx, "gh", "repo", "clone", spec, destBare, "--", "--bare")
	if err != nil {
		if stderr != "" {
			return fmt.Errorf("gh repo clone %s: %s", spec, stderr)
		}
		return fmt.Errorf("gh repo clone %s: %w", spec, err)
	}
	return nil
}
