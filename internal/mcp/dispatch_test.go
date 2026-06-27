package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func callResult(t *testing.T, resp map[string]json.RawMessage) toolResult {
	t.Helper()
	var tr toolResult
	if err := json.Unmarshal(resp["result"], &tr); err != nil {
		t.Fatalf("unmarshal toolResult: %v", err)
	}
	return tr
}

func TestCallListProjects(t *testing.T) {
	deps := Deps{ListProjects: func(ctx context.Context) ([]Project, error) {
		return []Project{{ID: "p1", DisplayName: "demo"}}, nil
	}}
	resp := run(t, deps, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_projects","arguments":{}}}`)
	tr := callResult(t, resp[0])
	if tr.IsError {
		t.Fatalf("unexpected error: %+v", tr)
	}
	if len(tr.Content) != 1 || tr.Content[0].Type != "text" {
		t.Fatalf("content = %+v", tr.Content)
	}
	var payload struct {
		Projects []Project `json:"projects"`
	}
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Projects) != 1 || payload.Projects[0].ID != "p1" {
		t.Fatalf("projects = %+v", payload.Projects)
	}
}

func TestCallCreateWorktreePassesArgs(t *testing.T) {
	var gotRef, gotName, gotAgent string
	deps := Deps{CreateWorktree: func(ctx context.Context, ref, name, agent string) (Worktree, error) {
		gotRef, gotName, gotAgent = ref, name, agent
		return Worktree{Name: name}, nil
	}}
	run(t, deps, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_worktree","arguments":{"project":"demo","name":"feat/x","agent":"claude"}}}`)
	if gotRef != "demo" || gotName != "feat/x" || gotAgent != "claude" {
		t.Fatalf("args = %q %q %q", gotRef, gotName, gotAgent)
	}
}

func TestCallToolErrorSetsIsError(t *testing.T) {
	deps := Deps{GetProject: func(ctx context.Context, ref string) (Project, error) {
		return Project{}, errors.New("session \"nope\" not found.")
	}}
	resp := run(t, deps, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_project","arguments":{"project":"nope"}}}`)
	tr := callResult(t, resp[0])
	if !tr.IsError {
		t.Fatal("want IsError=true")
	}
	if tr.Content[0].Text != "session \"nope\" not found." {
		t.Fatalf("text = %q", tr.Content[0].Text)
	}
}

func TestCallUnknownToolIsError(t *testing.T) {
	resp := run(t, Deps{}, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"bogus","arguments":{}}}`)
	tr := callResult(t, resp[0])
	if !tr.IsError {
		t.Fatal("want IsError=true for unknown tool")
	}
}
