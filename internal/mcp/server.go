package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yourname/sshops/internal/config"
	"github.com/yourname/sshops/internal/inventory"
	"github.com/yourname/sshops/internal/vault"
)

type Server struct {
	inventory *inventory.Inventory
	vault     *vault.Vault
	config    *config.Config
	tools     []ToolDef
}

type ToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolsCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type toolCallResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func NewServer(inv *inventory.Inventory, v *vault.Vault, cfg *config.Config) *Server {
	s := &Server{inventory: inv, vault: v, config: cfg}
	s.tools = s.buildToolDefs()
	return s
}

func (s *Server) Handle(req *JSONRPCRequest) *JSONRPCResponse {
	if req == nil {
		return &JSONRPCResponse{JSONRPC: "2.0", Error: &RPCError{Code: -32600, Message: "Invalid Request"}}
	}

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	default:
		if strings.HasPrefix(req.Method, "notifications/") {
			if req.ID == nil {
				return nil
			}
			return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{}}
		}
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &RPCError{Code: -32601, Message: "Method not found"}}
	}
}

func (s *Server) handleInitialize(req *JSONRPCRequest) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo": map[string]interface{}{
				"name":    "sshops",
				"version": "1.0.0",
			},
		},
	}
}

func (s *Server) handleToolsList(req *JSONRPCRequest) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"tools": s.tools,
		},
	}
}

func (s *Server) handleToolsCall(req *JSONRPCRequest) *JSONRPCResponse {
	var params toolsCallParams
	if len(req.Params) == 0 {
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &RPCError{Code: -32602, Message: "Invalid params"}}
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &RPCError{Code: -32602, Message: "Invalid params"}}
	}
	if params.Arguments == nil {
		params.Arguments = map[string]interface{}{}
	}

	text, err := s.callTool(params.Name, params.Arguments)
	result := toolCallResult{Content: []toolContent{{Type: "text", Text: text}}}
	if err != nil {
		result.IsError = true
		result.Content[0].Text = fmt.Sprintf("%v", err)
	}

	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
}
