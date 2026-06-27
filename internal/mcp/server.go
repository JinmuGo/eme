package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
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
		s.handleToolCall(req.ID, req.Params) // implemented in Task 2
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
	s.writeError(id, codeInternal, "tools/call not yet implemented")
}
