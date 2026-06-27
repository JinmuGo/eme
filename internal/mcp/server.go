package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

type server struct {
	deps Deps
	enc  *json.Encoder
	mu   sync.Mutex
}

// Serve runs the MCP server until in reaches EOF. Each line of in is one
// JSON-RPC message; each response is written to out as one compact JSON line.
func Serve(in io.Reader, out io.Writer, deps Deps) error {
	s := &server{deps: deps, enc: json.NewEncoder(out)}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024) // tolerate large messages
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		s.handleLine(line)
	}
	return sc.Err()
}

func (s *server) handleLine(line []byte) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		s.writeError(json.RawMessage("null"), codeParse, "parse error")
		return
	}
	isNotification := len(req.ID) == 0
	switch req.Method {
	case "initialize":
		s.writeResult(req.ID, s.handleInitialize(req.Params))
	case "notifications/initialized", "notifications/cancelled":
		// notifications: no response
	case "ping":
		if !isNotification {
			s.writeResult(req.ID, struct{}{})
		}
	case "tools/list":
		s.writeResult(req.ID, toolsListResult{Tools: toolDefs})
	case "tools/call":
		if !isNotification {
			s.handleToolCall(req.ID, req.Params)
		}
	default:
		if !isNotification {
			s.writeError(req.ID, codeMethodNotFound, "method not found: "+req.Method)
		}
	}
}

func (s *server) handleInitialize(params json.RawMessage) initializeResult {
	var p initializeParams
	_ = json.Unmarshal(params, &p)
	version := supportedProtocolVersion
	if p.ProtocolVersion != "" {
		version = p.ProtocolVersion // echo the client's requested version
	}
	return initializeResult{
		ProtocolVersion: version,
		Capabilities:    capabilities{Tools: toolsCapability{}},
		ServerInfo:      serverInfo{Name: "eme", Version: s.deps.ServerVersion},
	}
}

func (s *server) writeResult(id json.RawMessage, result interface{}) {
	s.write(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *server) writeError(id json.RawMessage, code int, msg string) {
	s.write(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

func (s *server) write(resp rpcResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.enc.Encode(resp) // Encoder writes compact JSON + trailing newline
}

func (s *server) handleToolCall(id json.RawMessage, params json.RawMessage) {
	var p callParams
	if err := json.Unmarshal(params, &p); err != nil {
		s.writeError(id, codeInvalidParams, "invalid params")
		return
	}
	s.writeResult(id, s.dispatchTool(context.Background(), p.Name, p.Arguments))
}

func okResult(v interface{}) toolResult {
	b, _ := json.Marshal(v)
	return toolResult{
		Content:           []contentBlock{{Type: "text", Text: string(b)}},
		StructuredContent: v,
	}
}

func errResult(err error) toolResult {
	return toolResult{
		Content: []contentBlock{{Type: "text", Text: err.Error()}},
		IsError: true,
	}
}

func (s *server) dispatchTool(ctx context.Context, name string, rawArgs json.RawMessage) toolResult {
	parse := func(dst interface{}) bool {
		if len(rawArgs) == 0 {
			return true
		}
		return json.Unmarshal(rawArgs, dst) == nil
	}

	switch name {
	case "list_projects":
		ps, err := s.deps.ListProjects(ctx)
		if err != nil {
			return errResult(err)
		}
		return okResult(map[string]interface{}{"projects": ps})

	case "get_project":
		var a struct {
			Project string `json:"project"`
		}
		if !parse(&a) {
			return errResult(fmt.Errorf("invalid arguments"))
		}
		p, err := s.deps.GetProject(ctx, a.Project)
		if err != nil {
			return errResult(err)
		}
		return okResult(map[string]interface{}{"project": p})

	case "read_worktree_output":
		var a struct {
			Project  string `json:"project"`
			Worktree string `json:"worktree"`
			Lines    int    `json:"lines"`
		}
		if !parse(&a) {
			return errResult(fmt.Errorf("invalid arguments"))
		}
		out, err := s.deps.ReadOutput(ctx, a.Project, a.Worktree, a.Lines)
		if err != nil {
			return errResult(err)
		}
		return okResult(map[string]interface{}{"output": out})

	case "create_project":
		var a struct {
			Folder string `json:"folder"`
			Agent  string `json:"agent"`
		}
		if !parse(&a) {
			return errResult(fmt.Errorf("invalid arguments"))
		}
		p, err := s.deps.CreateProject(ctx, a.Folder, a.Agent)
		if err != nil {
			return errResult(err)
		}
		return okResult(map[string]interface{}{"project": p})

	case "clone_repo":
		var a struct {
			Repo  string `json:"repo"`
			Agent string `json:"agent"`
		}
		if !parse(&a) {
			return errResult(fmt.Errorf("invalid arguments"))
		}
		p, err := s.deps.CloneRepo(ctx, a.Repo, a.Agent)
		if err != nil {
			return errResult(err)
		}
		return okResult(map[string]interface{}{"project": p})

	case "create_worktree":
		var a struct {
			Project string `json:"project"`
			Name    string `json:"name"`
			Agent   string `json:"agent"`
		}
		if !parse(&a) {
			return errResult(fmt.Errorf("invalid arguments"))
		}
		w, err := s.deps.CreateWorktree(ctx, a.Project, a.Name, a.Agent)
		if err != nil {
			return errResult(err)
		}
		return okResult(map[string]interface{}{"worktree": w})

	case "start_agent":
		var a struct {
			Project  string `json:"project"`
			Worktree string `json:"worktree"`
			Agent    string `json:"agent"`
		}
		if !parse(&a) {
			return errResult(fmt.Errorf("invalid arguments"))
		}
		r, err := s.deps.StartAgent(ctx, a.Project, a.Worktree, a.Agent)
		if err != nil {
			return errResult(err)
		}
		return okResult(r)

	case "stop_agent":
		var a struct {
			Project  string `json:"project"`
			Worktree string `json:"worktree"`
		}
		if !parse(&a) {
			return errResult(fmt.Errorf("invalid arguments"))
		}
		r, err := s.deps.StopAgent(ctx, a.Project, a.Worktree)
		if err != nil {
			return errResult(err)
		}
		return okResult(r)

	default:
		return errResult(fmt.Errorf("unknown tool: %s", name))
	}
}
