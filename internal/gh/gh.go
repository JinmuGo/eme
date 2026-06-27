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
	"strings"

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

// RepoList returns the authenticated user's own repositories, most-recently-updated
// first. It does NOT include repositories owned by organizations the user belongs to
// — `gh repo list` with no owner is scoped to the caller's account. Use RepoListAll
// for the merged set.
func RepoList(ctx context.Context, limit int) ([]Repo, error) {
	return RepoListOwner(ctx, "", limit)
}

// RepoListOwner returns repositories for owner (a user or organization login),
// most-recently-updated first. An empty owner lists the authenticated user's own
// repositories.
func RepoListOwner(ctx context.Context, owner string, limit int) ([]Repo, error) {
	args := []string{"repo", "list"}
	if owner != "" {
		args = append(args, owner)
	}
	args = append(args, "--limit", fmt.Sprintf("%d", limit),
		"--json", "nameWithOwner,description,isPrivate,updatedAt")
	out, _, err := Runner.Run(ctx, "gh", args...)
	if err != nil {
		return nil, fmt.Errorf("gh repo list: %w", err)
	}
	var repos []Repo
	if err := json.Unmarshal([]byte(out), &repos); err != nil {
		return nil, fmt.Errorf("decode gh repo list: %w", err)
	}
	sortByUpdatedDesc(repos)
	return repos, nil
}

// OrgList returns the login names of organizations the authenticated user belongs
// to. Seeing private memberships requires gh's token to carry the read:org scope;
// without it only orgs with a public membership appear — a graceful degradation,
// which is why callers treat any error here as "no orgs" rather than fatal.
func OrgList(ctx context.Context) ([]string, error) {
	out, _, err := Runner.Run(ctx, "gh", "api", "user/orgs", "--paginate", "--jq", ".[].login")
	if err != nil {
		return nil, fmt.Errorf("gh api user/orgs: %w", err)
	}
	var orgs []string
	for line := range strings.SplitSeq(out, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			orgs = append(orgs, s)
		}
	}
	return orgs, nil
}

// RepoListAll returns the authenticated user's own repositories plus repositories
// from every organization they belong to, deduplicated by nameWithOwner and sorted
// most-recently-updated first. Listing the user's own repos is fatal on failure (it
// signals a real auth/network problem); organization enumeration and per-org listing
// are best-effort, so a single inaccessible org never empties the picker.
func RepoListAll(ctx context.Context, limit int) ([]Repo, error) {
	own, err := RepoList(ctx, limit)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(own))
	for _, r := range own {
		seen[r.NameWithOwner] = true
	}
	merged := own

	orgs, err := OrgList(ctx)
	if err != nil {
		return own, nil // no read:org scope or transient failure: own repos still work
	}
	for _, org := range orgs {
		orgRepos, err := RepoListOwner(ctx, org, limit)
		if err != nil {
			continue // skip an org we cannot list; never fail the whole picker
		}
		for _, r := range orgRepos {
			if seen[r.NameWithOwner] {
				continue
			}
			seen[r.NameWithOwner] = true
			merged = append(merged, r)
		}
	}
	sortByUpdatedDesc(merged)
	return merged, nil
}

// sortByUpdatedDesc orders repos most-recently-updated first (RFC3339 sorts
// lexically by time).
func sortByUpdatedDesc(repos []Repo) {
	sort.SliceStable(repos, func(i, j int) bool {
		return repos[i].UpdatedAt > repos[j].UpdatedAt
	})
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
