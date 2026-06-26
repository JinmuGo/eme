package gh

import (
	"context"
	"errors"
	"testing"

	"github.com/JinmuGo/eme/internal/runner"
)

func TestRepoListArgsAndSort(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("gh", []string{"repo", "list", "--limit", "200", "--json", "nameWithOwner,description,isPrivate,updatedAt"},
		`[{"nameWithOwner":"JinmuGo/old","description":"o","isPrivate":false,"updatedAt":"2024-01-01T00:00:00Z"},`+
			`{"nameWithOwner":"JinmuGo/eme","description":"new","isPrivate":true,"updatedAt":"2026-06-01T00:00:00Z"}]`,
		"", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()

	repos, err := RepoList(context.Background(), 200)
	if err != nil {
		t.Fatalf("RepoList: %v", err)
	}
	if len(repos) != 2 || repos[0].NameWithOwner != "JinmuGo/eme" {
		t.Fatalf("want recently-updated first, got %+v", repos)
	}
	if !repos[0].IsPrivate || repos[0].Description != "new" {
		t.Errorf("decode mismatch: %+v", repos[0])
	}
}

func TestCloneBareArgs(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("gh", []string{"repo", "clone", "JinmuGo/eme", "/dst/.bare", "--", "--bare"}, "", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()

	if err := CloneBare(context.Background(), "JinmuGo/eme", "/dst/.bare"); err != nil {
		t.Fatalf("CloneBare: %v", err)
	}
}

func TestAuthed(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("gh", []string{"auth", "status"}, "", "", nil)
	Runner = mock
	defer func() { Runner = runner.Default }()
	if !Authed(context.Background()) {
		t.Error("Authed = false, want true on exit 0")
	}

	mock2 := runner.NewMock()
	mock2.Set("gh", []string{"auth", "status"}, "", "not logged in", errors.New("exit 1"))
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
