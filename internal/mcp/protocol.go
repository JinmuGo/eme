package mcp

import "encoding/json"

// JSON-RPC 2.0 framing.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // absent on notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

// MCP lifecycle types.
const supportedProtocolVersion = "2025-06-18"

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

type initializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    capabilities `json:"capabilities"`
	ServerInfo      serverInfo   `json:"serverInfo"`
}

type capabilities struct {
	Tools toolsCapability `json:"tools"`
}

type toolsCapability struct {
	ListChanged bool `json:"listChanged"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Tool is one entry in tools/list.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type toolsListResult struct {
	Tools []Tool `json:"tools"`
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type contentBlock struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}

type toolResult struct {
	Content           []contentBlock `json:"content"`
	StructuredContent interface{}    `json:"structuredContent,omitempty"`
	IsError           bool           `json:"isError,omitempty"`
}
