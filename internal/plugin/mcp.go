// Package plugin - MCP (Model Context Protocol) server registry.
// MCP servers expose external tools to the krill agent via JSON-RPC 2.0 over
// stdin/stdout. This is how krill extends its tentacles into the wider ecosystem -
// file systems, databases, APIs, and beyond.
// Krill fact: krill have 10 pairs of thoracic legs (thoracopods) for swimming and
// filter-feeding. MCP servers are the krill's digital thoracopods - each one
// reaches out to grab a different kind of resource.
package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// ---------------------------------------------------------------------------
// JSON-RPC 2.0 wire types - the protocol krill uses to talk to MCP servers
// ---------------------------------------------------------------------------

// jsonRPCRequest is an outgoing JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      *int64      `json:"id,omitempty"` // nil for notifications
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// jsonRPCResponse is an incoming JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError is the error object in a JSON-RPC 2.0 response.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// initializeParams is the params for the MCP initialize request.
type initializeParams struct {
	ProtocolVersion string           `json:"protocolVersion"`
	Capabilities    struct{}         `json:"capabilities"`
	ClientInfo      mcpClientInfo    `json:"clientInfo"`
}

// mcpClientInfo identifies this MCP client.
type mcpClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// toolsListResult is the result of tools/list.
type toolsListResult struct {
	Tools []mcpToolDef `json:"tools"`
}

// mcpToolDef is a tool definition from the MCP server.
type mcpToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
}

// callToolParams is the params for tools/call.
type callToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// callToolResult is the result of tools/call.
type callToolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// toolContent is a single content block in a tool call result.
type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ---------------------------------------------------------------------------
// MCP Server - a single connection to an MCP-compliant subprocess
// ---------------------------------------------------------------------------

// MCPServerImpl manages a connection to a single MCP server process.
// Communication happens via JSON-RPC 2.0 over the process's stdin/stdout.
// Like a krill's thoracopod reaching into the current to filter-feed,
// each MCP server reaches into an external system to extract capabilities.
type MCPServerImpl struct {
	mu        sync.Mutex
	name      string
	command   string
	args      []string
	env       map[string]string
	connected bool
	tools     []core.MCPTool

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	nextID atomic.Int64
}

// NewMCPServer creates a new MCP server connection manager.
// The server is not connected until Connect() is called.
func NewMCPServer(name string, command string, args []string, env map[string]string) *MCPServerImpl {
	s := &MCPServerImpl{
		name:    name,
		command: command,
		args:    args,
		env:     env,
	}
	// Start IDs at 1 - JSON-RPC convention
	s.nextID.Store(1)
	return s
}

// Name returns the server's identifier.
func (s *MCPServerImpl) Name() string { return s.name }

// Connect launches the MCP server subprocess and performs the initialization
// handshake: initialize -> initialized notification -> tools/list.
// This is the krill extending a new thoracopod into the water column.
func (s *MCPServerImpl) Connect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.connected {
		return fmt.Errorf("MCP server %q is already connected", s.name)
	}

	log.Info("connecting to MCP server", "name", s.name, "command", s.command)

	// Build the command
	cmd := exec.CommandContext(ctx, s.command, s.args...)

	// Set environment variables
	if len(s.env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range s.env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	// Capture stdin/stdout for JSON-RPC communication
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("MCP server %q stdin pipe: %w", s.name, err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("MCP server %q stdout pipe: %w", s.name, err)
	}

	// Discard stderr to avoid blocking - MCP servers may log there
	cmd.Stderr = io.Discard

	// Start the process
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return fmt.Errorf("MCP server %q start: %w", s.name, err)
	}

	s.cmd = cmd
	s.stdin = stdin
	s.reader = bufio.NewReader(stdout)

	// Step 1: Send initialize request
	initResp, err := s.sendRequest("initialize", &initializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities:    struct{}{},
		ClientInfo: mcpClientInfo{
			Name:    "mini-krill",
			Version: core.Version,
		},
	})
	if err != nil {
		s.cleanup()
		return fmt.Errorf("MCP server %q initialize failed: %w", s.name, err)
	}

	log.Debug("MCP server initialized", "name", s.name, "response", string(initResp.Result))

	// Step 2: Send initialized notification (no id = notification)
	if err := s.sendNotification("notifications/initialized"); err != nil {
		s.cleanup()
		return fmt.Errorf("MCP server %q initialized notification failed: %w", s.name, err)
	}

	// Step 3: List available tools
	toolsResp, err := s.sendRequest("tools/list", struct{}{})
	if err != nil {
		s.cleanup()
		return fmt.Errorf("MCP server %q tools/list failed: %w", s.name, err)
	}

	var toolsList toolsListResult
	if err := json.Unmarshal(toolsResp.Result, &toolsList); err != nil {
		s.cleanup()
		return fmt.Errorf("MCP server %q parse tools list: %w", s.name, err)
	}

	// Convert to core.MCPTool
	s.tools = make([]core.MCPTool, len(toolsList.Tools))
	for i, t := range toolsList.Tools {
		s.tools[i] = core.MCPTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}

	s.connected = true
	log.Info("MCP server connected", "name", s.name, "tools", len(s.tools))
	return nil
}

// Tools returns the list of tools available from this MCP server.
func (s *MCPServerImpl) Tools() []core.MCPTool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.connected {
		return nil
	}

	// Return a copy to prevent external mutation
	result := make([]core.MCPTool, len(s.tools))
	copy(result, s.tools)
	return result
}

// CallTool invokes a named tool on the MCP server with the given arguments.
// Like a krill's thoracopod catching a specific type of phytoplankton -
// each tool call targets a precise capability.
func (s *MCPServerImpl) CallTool(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.connected {
		return "", fmt.Errorf("MCP server %q is not connected", s.name)
	}

	log.Debug("calling MCP tool", "server", s.name, "tool", name)

	resp, err := s.sendRequest("tools/call", &callToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return "", fmt.Errorf("MCP tool %q call failed: %w", name, err)
	}

	var result callToolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("parse tool %q result: %w", name, err)
	}

	if result.IsError {
		// Collect error text from content blocks
		var errText string
		for _, c := range result.Content {
			if c.Type == "text" {
				errText += c.Text
			}
		}
		return "", fmt.Errorf("MCP tool %q returned error: %s", name, errText)
	}

	// Concatenate text content blocks
	var output string
	for _, c := range result.Content {
		if c.Type == "text" {
			output += c.Text
		}
	}

	return output, nil
}

// Close terminates the MCP server process gracefully.
// First closes stdin to signal EOF, then waits for the process to exit.
// Like a krill retracting a thoracopod - clean and orderly.
func (s *MCPServerImpl) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.connected {
		return nil
	}

	log.Debug("closing MCP server", "name", s.name)
	s.cleanup()
	log.Info("MCP server closed", "name", s.name)
	return nil
}

// sendRequest sends a JSON-RPC 2.0 request and waits for the response.
// Must be called with s.mu held.
func (s *MCPServerImpl) sendRequest(method string, params interface{}) (*jsonRPCResponse, error) {
	id := s.nextID.Add(1) - 1

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  method,
		Params:  params,
	}

	if err := s.writeJSON(req); err != nil {
		return nil, fmt.Errorf("send %s: %w", method, err)
	}

	// Read response - keep reading lines until we get a valid JSON-RPC response
	// with a matching ID. MCP servers may emit notifications between request/response.
	for {
		line, err := s.reader.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("read response for %s: %w", method, err)
		}

		// Skip empty lines
		if len(line) == 0 || string(line) == "\n" {
			continue
		}

		var resp jsonRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			// Not valid JSON - skip (could be debug output)
			log.Debug("skipping non-JSON line from MCP server", "name", s.name, "line", string(line))
			continue
		}

		// Skip notifications (no id)
		if resp.ID == nil {
			continue
		}

		// Check for JSON-RPC error
		if resp.Error != nil {
			return nil, fmt.Errorf("JSON-RPC error (code %d): %s", resp.Error.Code, resp.Error.Message)
		}

		return &resp, nil
	}
}

// sendNotification sends a JSON-RPC 2.0 notification (no id, no response expected).
// Must be called with s.mu held.
func (s *MCPServerImpl) sendNotification(method string) error {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
	}
	return s.writeJSON(req)
}

// writeJSON marshals and writes a JSON-RPC message to the server's stdin.
// Each message is terminated with a newline.
// Must be called with s.mu held.
func (s *MCPServerImpl) writeJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal JSON-RPC: %w", err)
	}

	data = append(data, '\n')
	if _, err := s.stdin.Write(data); err != nil {
		return fmt.Errorf("write to MCP server stdin: %w", err)
	}

	return nil
}

// cleanup terminates the subprocess and resets connection state.
// Must be called with s.mu held.
func (s *MCPServerImpl) cleanup() {
	s.connected = false

	if s.stdin != nil {
		s.stdin.Close()
		s.stdin = nil
	}

	if s.cmd != nil && s.cmd.Process != nil {
		// Send SIGTERM for graceful shutdown, then wait
		_ = s.cmd.Process.Signal(os.Interrupt)
		// Wait for the process to exit (non-blocking if already dead)
		_ = s.cmd.Wait()
		s.cmd = nil
	}

	s.reader = nil
	s.tools = nil
}

// ---------------------------------------------------------------------------
// MCP Registry - manages the fleet of MCP server connections
// ---------------------------------------------------------------------------

// MCPRegistryImpl is the concrete implementation of core.MCPRegistry.
// Manages multiple MCP server connections, providing unified access to all
// tools across all connected servers.
// Like a krill swarm coordinator - knows where every member is and what
// they can do.
type MCPRegistryImpl struct {
	mu      sync.RWMutex
	servers map[string]core.MCPServer
}

// NewMCPRegistry creates a new, empty MCP server registry.
func NewMCPRegistry() *MCPRegistryImpl {
	return &MCPRegistryImpl{
		servers: make(map[string]core.MCPServer),
	}
}

// Register adds an MCP server to the registry. Does not automatically connect it.
func (r *MCPRegistryImpl) Register(name string, server core.MCPServer) error {
	if server == nil {
		return fmt.Errorf("cannot register nil MCP server")
	}
	if name == "" {
		return fmt.Errorf("cannot register MCP server with empty name")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.servers[name]; exists {
		return fmt.Errorf("MCP server %q already registered", name)
	}

	r.servers[name] = server
	log.Debug("MCP server registered", "name", name)
	return nil
}

// Get retrieves an MCP server by name.
func (r *MCPRegistryImpl) Get(name string) (core.MCPServer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	server, ok := r.servers[name]
	return server, ok
}

// List returns metadata for all registered MCP servers.
func (r *MCPRegistryImpl) List() []core.MCPServerInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]core.MCPServerInfo, 0, len(r.servers))
	for name, server := range r.servers {
		tools := server.Tools()
		toolCount := 0
		if tools != nil {
			toolCount = len(tools)
		}

		// Determine connection status by checking if tools are available.
		// A connected server will have a non-nil tools list (possibly empty).
		connected := tools != nil

		infos = append(infos, core.MCPServerInfo{
			Name:      name,
			Connected: connected,
			ToolCount: toolCount,
		})
	}

	return infos
}

// AllTools returns tools from all connected MCP servers, aggregated into
// a single slice. This is the unified capability surface the agent sees -
// one swarm, many tools.
func (r *MCPRegistryImpl) AllTools() []core.MCPTool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var all []core.MCPTool
	for _, server := range r.servers {
		tools := server.Tools()
		if tools != nil {
			all = append(all, tools...)
		}
	}

	return all
}

// Close terminates all MCP server connections gracefully.
// Errors from individual servers are logged but do not prevent
// closing the remaining ones - the swarm disperses gracefully.
func (r *MCPRegistryImpl) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for name, server := range r.servers {
		if err := server.Close(); err != nil {
			log.Error("failed to close MCP server", "name", name, "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	// Clear the map after closing everything
	r.servers = make(map[string]core.MCPServer)
	return firstErr
}

// LoadFromConfig creates MCP servers from the configuration and optionally
// connects them. Servers with enabled=false are skipped.
// Like krill hatching from eggs at depth - each server starts its lifecycle
// from config and connects when conditions are right.
func (r *MCPRegistryImpl) LoadFromConfig(servers map[string]config.MCPServerConfig) error {
	for name, cfg := range servers {
		if !cfg.Enabled {
			log.Debug("skipping disabled MCP server", "name", name)
			continue
		}

		if cfg.Command == "" {
			log.Warn("MCP server has no command, skipping", "name", name)
			continue
		}

		server := NewMCPServer(name, cfg.Command, cfg.Args, cfg.Env)
		if err := r.Register(name, server); err != nil {
			log.Warn("failed to register MCP server from config", "name", name, "error", err)
			continue
		}

		log.Info("MCP server loaded from config", "name", name, "command", cfg.Command)
	}

	return nil
}
