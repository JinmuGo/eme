package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// run feeds newline-joined requests through Serve and returns the response lines.
func run(t *testing.T, deps Deps, requests ...string) []map[string]json.RawMessage {
	t.Helper()
	in := strings.NewReader(strings.Join(requests, "\n") + "\n")
	var out strings.Builder
	if err := Serve(in, &out, deps); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var got []map[string]json.RawMessage
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bad response line %q: %v", line, err)
		}
		got = append(got, m)
	}
	return got
}

func TestInitialize(t *testing.T) {
	resp := run(t, Deps{ServerVersion: "test"},
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	if len(resp) != 1 {
		t.Fatalf("want 1 response, got %d", len(resp))
	}
	var res initializeResult
	if err := json.Unmarshal(resp[0]["result"], &res); err != nil {
		t.Fatal(err)
	}
	if res.ServerInfo.Name != "eme" || res.ServerInfo.Version != "test" {
		t.Fatalf("serverInfo = %+v", res.ServerInfo)
	}
	if res.ProtocolVersion != "2025-06-18" {
		t.Fatalf("protocolVersion = %q", res.ProtocolVersion)
	}
}

func TestToolsListReturnsEightTools(t *testing.T) {
	resp := run(t, Deps{}, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	var res toolsListResult
	if err := json.Unmarshal(resp[0]["result"], &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) != 8 {
		t.Fatalf("want 8 tools, got %d", len(res.Tools))
	}
}

func TestUnknownMethodIsError(t *testing.T) {
	resp := run(t, Deps{}, `{"jsonrpc":"2.0","id":3,"method":"bogus"}`)
	var e rpcError
	if err := json.Unmarshal(resp[0]["error"], &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != codeMethodNotFound {
		t.Fatalf("code = %d", e.Code)
	}
}

func TestNotificationProducesNoResponse(t *testing.T) {
	resp := run(t, Deps{}, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if len(resp) != 0 {
		t.Fatalf("want 0 responses, got %d", len(resp))
	}
}

func TestParseErrorOnBadJSON(t *testing.T) {
	resp := run(t, Deps{}, `{not json`)
	var e rpcError
	if err := json.Unmarshal(resp[0]["error"], &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != codeParse {
		t.Fatalf("code = %d", e.Code)
	}
}

var _ = context.Background // keep context imported for Task 2 tests
