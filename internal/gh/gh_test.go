package gh

import (
	"context"
	"errors"
	"testing"

	"github.com/alderwork/eme/internal/runner"
)

func TestRepoListArgsAndSort(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("gh", []string{"repo", "list", "--limit", "200", "--json", "nameWithOwner,description,isPrivate,updatedAt"},
		`[{"nameWithOwner":"JinmuGo/old","description":"o","isPrivate":false,"updatedAt":"2024-01-01T00:00:00Z"},`+
			`{"nameWithOwner":"alderwork/eme","description":"new","isPrivate":true,"updatedAt":"2026-06-01T00:00:00Z"}]`,
		"", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()

	repos, err := RepoList(context.Background(), 200)
	if err != nil {
		t.Fatalf("RepoList: %v", err)
	}
	if len(repos) != 2 || repos[0].NameWithOwner != "alderwork/eme" {
		t.Fatalf("want recently-updated first, got %+v", repos)
	}
	if !repos[0].IsPrivate || repos[0].Description != "new" {
		t.Errorf("decode mismatch: %+v", repos[0])
	}
}

func TestOrgList(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("gh", []string{"api", "user/orgs", "--paginate", "--jq", ".[].login"},
		"alderwork\nacme\n", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()

	orgs, err := OrgList(context.Background())
	if err != nil {
		t.Fatalf("OrgList: %v", err)
	}
	if len(orgs) != 2 || orgs[0] != "alderwork" || orgs[1] != "acme" {
		t.Fatalf("orgs = %v, want [alderwork acme]", orgs)
	}
}

func TestRepoListOwnerPassesOwner(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("gh", []string{"repo", "list", "alderwork", "--limit", "100", "--json", "nameWithOwner,description,isPrivate,updatedAt"},
		`[{"nameWithOwner":"alderwork/eme","description":"d","isPrivate":true,"updatedAt":"2026-05-01T00:00:00Z"}]`,
		"", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()

	repos, err := RepoListOwner(context.Background(), "alderwork", 100)
	if err != nil {
		t.Fatalf("RepoListOwner: %v", err)
	}
	if len(repos) != 1 || repos[0].NameWithOwner != "alderwork/eme" {
		t.Fatalf("repos = %+v, want one alderwork/eme", repos)
	}
}

func TestRepoListAllMergesDedupsAndSorts(t *testing.T) {
	mock := runner.NewMock()
	// Own repos.
	mock.Set("gh", []string{"repo", "list", "--limit", "100", "--json", "nameWithOwner,description,isPrivate,updatedAt"},
		`[{"nameWithOwner":"me/own","description":"","isPrivate":false,"updatedAt":"2026-03-01T00:00:00Z"}]`,
		"", nil)
	// Orgs the user belongs to.
	mock.Set("gh", []string{"api", "user/orgs", "--paginate", "--jq", ".[].login"},
		"alderwork\nbroken\n", "", nil)
	// alderwork's repos: one newer than own, plus a duplicate of an own repo.
	mock.Set("gh", []string{"repo", "list", "alderwork", "--limit", "100", "--json", "nameWithOwner,description,isPrivate,updatedAt"},
		`[{"nameWithOwner":"alderwork/eme","description":"","isPrivate":true,"updatedAt":"2026-06-01T00:00:00Z"},`+
			`{"nameWithOwner":"me/own","description":"","isPrivate":false,"updatedAt":"2020-01-01T00:00:00Z"}]`,
		"", nil)
	// A second org we cannot list must be skipped, never fatal.
	mock.Set("gh", []string{"repo", "list", "broken", "--limit", "100", "--json", "nameWithOwner,description,isPrivate,updatedAt"},
		"", "no access", errors.New("exit 1"))
	Runner = mock
	defer func() { Runner = runner.Default }()

	repos, err := RepoListAll(context.Background(), 100)
	if err != nil {
		t.Fatalf("RepoListAll: %v", err)
	}
	// Expect own + alderwork/eme, deduped (me/own appears once), newest first.
	if len(repos) != 2 {
		t.Fatalf("repos = %+v, want 2 (deduped)", repos)
	}
	if repos[0].NameWithOwner != "alderwork/eme" || repos[1].NameWithOwner != "me/own" {
		t.Fatalf("order = %s,%s, want alderwork/eme,me/own", repos[0].NameWithOwner, repos[1].NameWithOwner)
	}
}

func TestRepoListAllDegradesWhenOrgListFails(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("gh", []string{"repo", "list", "--limit", "100", "--json", "nameWithOwner,description,isPrivate,updatedAt"},
		`[{"nameWithOwner":"me/own","description":"","isPrivate":false,"updatedAt":"2026-03-01T00:00:00Z"}]`,
		"", nil)
	// Org enumeration fails (e.g. missing read:org scope) — must still return own repos.
	mock.Set("gh", []string{"api", "user/orgs", "--paginate", "--jq", ".[].login"},
		"", "scope error", errors.New("exit 1"))
	Runner = mock
	defer func() { Runner = runner.Default }()

	repos, err := RepoListAll(context.Background(), 100)
	if err != nil {
		t.Fatalf("RepoListAll: %v", err)
	}
	if len(repos) != 1 || repos[0].NameWithOwner != "me/own" {
		t.Fatalf("repos = %+v, want only me/own", repos)
	}
}

func TestRepoListAllFatalWhenOwnReposFail(t *testing.T) {
	mock := runner.NewMock()
	// Listing the user's OWN repos (no owner) fails: a real auth/network problem must be
	// fatal, never silently degraded to an empty picker. RepoListAll returns before OrgList,
	// so no user/orgs mock is needed.
	mock.Set("gh", []string{"repo", "list", "--limit", "100", "--json", "nameWithOwner,description,isPrivate,updatedAt"},
		"", "could not connect", errors.New("exit 1"))
	Runner = mock
	defer func() { Runner = runner.Default }()

	repos, err := RepoListAll(context.Background(), 100)
	if err == nil {
		t.Fatalf("RepoListAll: want error when own repo list fails, got repos=%+v", repos)
	}
	if repos != nil {
		t.Fatalf("repos = %+v, want nil on fatal own-repo failure", repos)
	}
}

func TestCloneBareArgs(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("gh", []string{"repo", "clone", "alderwork/eme", "/dst/.bare", "--", "--bare"}, "", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()

	if err := CloneBare(context.Background(), "alderwork/eme", "/dst/.bare"); err != nil {
		t.Fatalf("CloneBare: %v", err)
	}
}

func TestAuthed(t *testing.T) {
	// Authed must key on the ACTIVE account only (--active): plain `gh auth status`
	// exits non-zero when any OTHER stored account has an invalid token, which would
	// wrongly report a working active account as unauthenticated.
	mock := runner.NewMock()
	mock.Set("gh", []string{"auth", "status", "--active"}, "", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()
	if !Authed(context.Background()) {
		t.Error("Authed = false, want true on exit 0")
	}

	mock2 := runner.NewMock()
	mock2.Set("gh", []string{"auth", "status", "--active"}, "", "not logged in", errors.New("exit 1"))
	Runner = mock2
	if Authed(context.Background()) {
		t.Error("Authed = true, want false on non-zero exit")
	}
}

func TestAvailable(t *testing.T) {
	LookPath = func(string) (string, error) { return "/usr/bin/gh", nil }
	defer func() { LookPath = origLookPath }()
	if !Available() {
		t.Error("Available = false, want true when on PATH")
	}
	LookPath = func(string) (string, error) { return "", errors.New("not found") }
	if Available() {
		t.Error("Available = true, want false when missing")
	}
}
