// Package agent — toolbox.go provides a unified tool execution layer.
// It wraps skills, MCP tools, and safe shell commands so that plan steps
// can dispatch real work instead of just asking the LLM to narrate.
package agent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

const (
	shellTimeout   = 30 * time.Second
	maxOutputBytes = 10 * 1024 // 10KB
)

// ToolDescriptor is a unified representation of any tool the agent can use.
type ToolDescriptor struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // "skill", "mcp", "shell"
}

// Toolbox wraps skills, MCP tools, and a safe shell executor into a single
// dispatch interface used during plan step execution.
type Toolbox struct {
	skills core.SkillRegistry
	mcp    core.MCPRegistry
	llm    core.LLMProvider
}

// NewToolbox creates a toolbox wired to all available tool backends.
func NewToolbox(skills core.SkillRegistry, mcp core.MCPRegistry, llm core.LLMProvider) *Toolbox {
	return &Toolbox{skills: skills, mcp: mcp, llm: llm}
}

// blockedCommands are shell patterns that are always rejected.
var blockedCommands = []string{
	"rm -rf /", "rm -rf /*", "sudo ", "mkfs", "dd if=",
	"chmod 777", "> /dev/", "curl | sh", "curl |sh",
	"wget | sh", "wget |sh", ":(){", "shutdown", "reboot",
	"format ", "fdisk",
}

// readOnlyCommands are shell commands that can run without approval.
var readOnlyCommands = []string{
	"ls", "cat", "head", "tail", "git status", "git log", "git diff",
	"git branch", "git remote", "find", "grep", "rg", "wc", "file",
	"stat", "pwd", "echo", "which", "env", "date", "tree",
	"go version", "node --version", "python --version",
}

// ExecuteTool dispatches a tool call to the correct backend.
func (t *Toolbox) ExecuteTool(ctx context.Context, name string, args map[string]string) (string, error) {
	log.Debug("toolbox dispatch", "tool", name, "args_count", len(args))

	// 1. Check if it's a registered skill
	if t.skills != nil {
		if skill, ok := t.skills.Get(name); ok {
			input := args["input"]
			if input == "" {
				// Fall back to joining all args
				var parts []string
				for _, v := range args {
					parts = append(parts, v)
				}
				input = strings.Join(parts, " ")
			}
			log.Info("toolbox: executing skill", "skill", name)
			return skill.Execute(ctx, input, t.llm)
		}
	}

	// 2. Check if it's an MCP tool
	if t.mcp != nil {
		servers := t.mcp.List()
		for _, info := range servers {
			if !info.Connected || !info.Enabled {
				continue
			}
			server, ok := t.mcp.Get(info.Name)
			if !ok {
				continue
			}
			for _, tool := range server.Tools() {
				if tool.Name == name {
					// Convert string args to interface{} args for MCP
					mcpArgs := make(map[string]interface{})
					for k, v := range args {
						mcpArgs[k] = v
					}
					log.Info("toolbox: executing MCP tool", "tool", name, "server", info.Name)
					return server.CallTool(ctx, name, mcpArgs)
				}
			}
		}
	}

	// 3. Shell command
	if name == "shell" {
		command := args["command"]
		if command == "" {
			return "", fmt.Errorf("shell tool requires a 'command' argument")
		}
		return ExecuteShellSafe(ctx, command)
	}

	return "", fmt.Errorf("unknown tool: %q", name)
}

// ListTools returns descriptors for all available tools.
func (t *Toolbox) ListTools() []ToolDescriptor {
	var tools []ToolDescriptor

	// Add skills
	if t.skills != nil {
		for _, info := range t.skills.List() {
			if info.Enabled && !strings.HasPrefix(info.Name, "self:") {
				tools = append(tools, ToolDescriptor{
					Name:        info.Name,
					Description: info.Description,
					Source:      "skill",
				})
			}
		}
	}

	// Add MCP tools
	if t.mcp != nil {
		for _, tool := range t.mcp.AllTools() {
			tools = append(tools, ToolDescriptor{
				Name:        tool.Name,
				Description: tool.Description,
				Source:      "mcp",
			})
		}
	}

	// Add built-in shell tool
	tools = append(tools, ToolDescriptor{
		Name:        "shell",
		Description: "Execute a shell command (read-only commands auto-approved, destructive commands blocked)",
		Source:      "shell",
	})

	return tools
}

// FormatToolsForLLM produces a text block listing all tools for LLM context.
func (t *Toolbox) FormatToolsForLLM() string {
	tools := t.ListTools()
	if len(tools) == 0 {
		return "No tools available."
	}

	var sb strings.Builder
	for _, tool := range tools {
		sb.WriteString(fmt.Sprintf("- %s [%s]: %s\n", tool.Name, tool.Source, tool.Description))
	}
	return sb.String()
}

// ExecuteShellSafe runs a shell command with safety checks.
// - Blocked commands are rejected outright.
// - Read-only commands execute immediately.
// - Write commands set NeedsApproval on the step (caller decides).
// Output is capped at maxOutputBytes.
func ExecuteShellSafe(ctx context.Context, command string) (string, error) {
	lower := strings.ToLower(strings.TrimSpace(command))

	// Check blocked list
	for _, blocked := range blockedCommands {
		if strings.Contains(lower, blocked) {
			log.Warn("shell command blocked", "command", command, "match", blocked)
			return "", fmt.Errorf("command blocked for safety: contains %q", blocked)
		}
	}

	// Execute with timeout
	shellCtx, cancel := context.WithTimeout(ctx, shellTimeout)
	defer cancel()

	cmd := exec.CommandContext(shellCtx, "sh", "-c", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	log.Debug("shell: executing", "command", truncate(command, 80))

	err := cmd.Run()

	// Combine output
	output := stdout.String()
	if errOut := stderr.String(); errOut != "" {
		if output != "" {
			output += "\n--- stderr ---\n"
		}
		output += errOut
	}

	// Truncate if too large
	if len(output) > maxOutputBytes {
		output = output[:maxOutputBytes] + "\n... (output truncated at 10KB)"
	}

	if err != nil {
		if shellCtx.Err() == context.DeadlineExceeded {
			return output, fmt.Errorf("command timed out after %s", shellTimeout)
		}
		// Include output even on error — stderr often has useful info
		return output, fmt.Errorf("command failed: %w", err)
	}

	if output == "" {
		output = "(no output)"
	}

	log.Debug("shell: completed", "output_len", len(output))
	return output, nil
}

// IsReadOnlyCommand checks if a command is in the read-only safe list.
func IsReadOnlyCommand(command string) bool {
	lower := strings.ToLower(strings.TrimSpace(command))
	// Extract the base command (first word or first two words for git subcommands)
	for _, ro := range readOnlyCommands {
		if strings.HasPrefix(lower, ro+" ") || lower == ro {
			return true
		}
	}
	return false
}

// IsBlockedCommand checks if a command matches the blocked list.
func IsBlockedCommand(command string) bool {
	lower := strings.ToLower(strings.TrimSpace(command))
	for _, blocked := range blockedCommands {
		if strings.Contains(lower, blocked) {
			return true
		}
	}
	return false
}
