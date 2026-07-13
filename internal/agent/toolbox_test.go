package agent

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/srvsngh99/mini-krill/internal/core"
)

// ---------------------------------------------------------------------------
// Mock skill for toolbox tests
// ---------------------------------------------------------------------------

type mockSkill struct {
	name   string
	desc   string
	result string
}

func (s *mockSkill) Name() string        { return s.name }
func (s *mockSkill) Description() string { return s.desc }
func (s *mockSkill) Execute(_ context.Context, input string, _ core.LLMProvider) (string, error) {
	return s.result + " (input: " + input + ")", nil
}

type toolboxSkillRegistry struct {
	skills map[string]core.Skill
}

func newToolboxSkillRegistry(skills ...core.Skill) *toolboxSkillRegistry {
	r := &toolboxSkillRegistry{skills: make(map[string]core.Skill)}
	for _, s := range skills {
		r.skills[s.Name()] = s
	}
	return r
}

func (r *toolboxSkillRegistry) Register(s core.Skill) error       { r.skills[s.Name()] = s; return nil }
func (r *toolboxSkillRegistry) Unregister(name string) error      { delete(r.skills, name); return nil }
func (r *toolboxSkillRegistry) SetEnabled(_ string, _ bool) error { return nil }
func (r *toolboxSkillRegistry) IsEnabled(_ string) bool           { return true }
func (r *toolboxSkillRegistry) Get(name string) (core.Skill, bool) {
	s, ok := r.skills[name]
	return s, ok
}
func (r *toolboxSkillRegistry) List() []core.SkillInfo {
	var infos []core.SkillInfo
	for _, s := range r.skills {
		infos = append(infos, core.SkillInfo{
			Name:        s.Name(),
			Description: s.Description(),
			Enabled:     true,
		})
	}
	return infos
}

// ---------------------------------------------------------------------------
// Toolbox: ExecuteTool tests
// ---------------------------------------------------------------------------

func TestToolboxExecuteSkill(t *testing.T) {
	registry := newToolboxSkillRegistry(
		&mockSkill{name: "search", desc: "Web search", result: "search results"},
		&mockSkill{name: "time", desc: "Current time", result: "2026-05-10 12:00"},
	)
	tb := NewToolbox(registry, nil, &MockProvider{chatResponse: "ok"})

	result, err := tb.ExecuteTool(context.Background(), "search", map[string]string{"input": "Go testing"})
	if err != nil {
		t.Fatalf("ExecuteTool(search) error: %v", err)
	}
	if !strings.Contains(result, "search results") {
		t.Errorf("expected search results, got: %s", result)
	}
	if !strings.Contains(result, "Go testing") {
		t.Errorf("expected input to be passed through, got: %s", result)
	}
}

func TestToolboxExecuteSkillFallbackArgs(t *testing.T) {
	registry := newToolboxSkillRegistry(
		&mockSkill{name: "time", desc: "Current time", result: "2026-05-10"},
	)
	tb := NewToolbox(registry, nil, &MockProvider{chatResponse: "ok"})

	// When no "input" key, args should be joined
	result, err := tb.ExecuteTool(context.Background(), "time", map[string]string{"foo": "bar"})
	if err != nil {
		t.Fatalf("ExecuteTool(time) error: %v", err)
	}
	if !strings.Contains(result, "2026-05-10") {
		t.Errorf("expected time result, got: %s", result)
	}
}

func TestToolboxShellReadOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell tests require Unix")
	}

	tb := NewToolbox(nil, nil, nil)
	result, err := tb.ExecuteTool(context.Background(), "shell", map[string]string{"command": "echo hello"})
	if err != nil {
		t.Fatalf("ExecuteTool(shell echo) error: %v", err)
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("expected 'hello' in output, got: %s", result)
	}
}

func TestToolboxShellPwd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell tests require Unix")
	}

	tb := NewToolbox(nil, nil, nil)
	result, err := tb.ExecuteTool(context.Background(), "shell", map[string]string{"command": "pwd"})
	if err != nil {
		t.Fatalf("ExecuteTool(shell pwd) error: %v", err)
	}
	if result == "" || result == "(no output)" {
		t.Error("pwd should produce output")
	}
}

func TestToolboxShellBlocked(t *testing.T) {
	tb := NewToolbox(nil, nil, nil)
	_, err := tb.ExecuteTool(context.Background(), "shell", map[string]string{"command": "rm -rf /"})
	if err == nil {
		t.Fatal("rm -rf / should be blocked")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected 'blocked' in error, got: %v", err)
	}
}

func TestToolboxShellBlockedSudo(t *testing.T) {
	tb := NewToolbox(nil, nil, nil)
	_, err := tb.ExecuteTool(context.Background(), "shell", map[string]string{"command": "sudo rm -rf /"})
	if err == nil {
		t.Fatal("sudo should be blocked")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("expected 'blocked' in error, got: %v", err)
	}
}

func TestToolboxShellMissingCommand(t *testing.T) {
	tb := NewToolbox(nil, nil, nil)
	_, err := tb.ExecuteTool(context.Background(), "shell", map[string]string{})
	if err == nil {
		t.Fatal("shell with empty command should error")
	}
	if !strings.Contains(err.Error(), "requires") {
		t.Errorf("expected 'requires' in error, got: %v", err)
	}
}

func TestToolboxUnknownTool(t *testing.T) {
	tb := NewToolbox(nil, nil, nil)
	_, err := tb.ExecuteTool(context.Background(), "nonexistent", map[string]string{})
	if err == nil {
		t.Fatal("unknown tool should error")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("expected 'unknown tool' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Toolbox: ListTools tests
// ---------------------------------------------------------------------------

func TestToolboxListToolsIncludesSkills(t *testing.T) {
	registry := newToolboxSkillRegistry(
		&mockSkill{name: "search", desc: "Web search", result: "results"},
		&mockSkill{name: "time", desc: "Current time", result: "now"},
	)
	tb := NewToolbox(registry, nil, nil)
	tools := tb.ListTools()

	// Should include search, time, and shell
	if len(tools) < 3 {
		t.Errorf("expected at least 3 tools, got %d", len(tools))
	}

	found := map[string]bool{}
	for _, tool := range tools {
		found[tool.Name] = true
	}
	for _, expected := range []string{"search", "time", "shell"} {
		if !found[expected] {
			t.Errorf("expected tool %q in list", expected)
		}
	}
}

func TestToolboxListToolsExcludesSelfSkills(t *testing.T) {
	registry := newToolboxSkillRegistry(
		&mockSkill{name: "self:health", desc: "Health check", result: "ok"},
		&mockSkill{name: "search", desc: "Web search", result: "results"},
	)
	tb := NewToolbox(registry, nil, nil)
	tools := tb.ListTools()

	for _, tool := range tools {
		if strings.HasPrefix(tool.Name, "self:") {
			t.Errorf("self:* skills should be excluded from tool list, found: %s", tool.Name)
		}
	}
}

func TestToolboxFormatToolsForLLM(t *testing.T) {
	registry := newToolboxSkillRegistry(
		&mockSkill{name: "search", desc: "Web search", result: "results"},
	)
	tb := NewToolbox(registry, nil, nil)
	formatted := tb.FormatToolsForLLM()

	if !strings.Contains(formatted, "search") {
		t.Errorf("formatted tools should contain 'search', got: %s", formatted)
	}
	if !strings.Contains(formatted, "shell") {
		t.Errorf("formatted tools should contain 'shell', got: %s", formatted)
	}
}

// ---------------------------------------------------------------------------
// IsReadOnlyCommand / IsBlockedCommand
// ---------------------------------------------------------------------------

func TestIsReadOnlyCommand(t *testing.T) {
	readOnly := []string{"ls /tmp", "cat file.txt", "git status", "git log", "pwd", "echo hello"}
	for _, cmd := range readOnly {
		if !IsReadOnlyCommand(cmd) {
			t.Errorf("IsReadOnlyCommand(%q) = false, want true", cmd)
		}
	}

	notReadOnly := []string{"rm file.txt", "make build", "npm install", "docker run foo"}
	for _, cmd := range notReadOnly {
		if IsReadOnlyCommand(cmd) {
			t.Errorf("IsReadOnlyCommand(%q) = true, want false", cmd)
		}
	}
}

func TestIsBlockedCommand(t *testing.T) {
	blocked := []string{"rm -rf /", "sudo cat /etc/passwd", "mkfs /dev/sda", "shutdown -h now"}
	for _, cmd := range blocked {
		if !IsBlockedCommand(cmd) {
			t.Errorf("IsBlockedCommand(%q) = false, want true", cmd)
		}
	}

	notBlocked := []string{"ls /tmp", "cat file.txt", "git status"}
	for _, cmd := range notBlocked {
		if IsBlockedCommand(cmd) {
			t.Errorf("IsBlockedCommand(%q) = true, want false", cmd)
		}
	}
}

// ---------------------------------------------------------------------------
// parseToolCall
// ---------------------------------------------------------------------------

func TestParseToolCall(t *testing.T) {
	response := "I need to check the files.\nTOOL_CALL: shell\nARGS: {\"command\": \"ls /tmp\"}\nLet me analyze."
	name, args := parseToolCall(response)
	if name != "shell" {
		t.Errorf("parseToolCall name = %q, want 'shell'", name)
	}
	if args["command"] != "ls /tmp" {
		t.Errorf("parseToolCall args[command] = %q, want 'ls /tmp'", args["command"])
	}
}

func TestParseToolCallNoTool(t *testing.T) {
	response := "Here is my analysis of the code..."
	name, _ := parseToolCall(response)
	if name != "" {
		t.Errorf("parseToolCall should return empty for no TOOL_CALL, got: %q", name)
	}
}

func TestParseToolCallSkillName(t *testing.T) {
	response := "TOOL_CALL: search\nARGS: {\"input\": \"Go error handling\"}"
	name, args := parseToolCall(response)
	if name != "search" {
		t.Errorf("name = %q, want 'search'", name)
	}
	if args["input"] != "Go error handling" {
		t.Errorf("args[input] = %q, want 'Go error handling'", args["input"])
	}
}

// ---------------------------------------------------------------------------
// parsePlanResponse with tool hints
// ---------------------------------------------------------------------------

func TestParsePlanResponseToolHints(t *testing.T) {
	response := "SUMMARY: Check the repo\nSTEP 1: List files [tool:shell]\nSTEP 2: Read README [tool:shell]\nSTEP 3: Analyze findings"
	plan := parsePlanResponse("check repo", response)

	if len(plan.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(plan.Steps))
	}

	if plan.Steps[0].ToolHint != "shell" {
		t.Errorf("step 1 ToolHint = %q, want 'shell'", plan.Steps[0].ToolHint)
	}
	if plan.Steps[1].ToolHint != "shell" {
		t.Errorf("step 2 ToolHint = %q, want 'shell'", plan.Steps[1].ToolHint)
	}
	if plan.Steps[2].ToolHint != "" {
		t.Errorf("step 3 ToolHint = %q, want empty", plan.Steps[2].ToolHint)
	}
	// Verify the [tool:shell] annotation is stripped from description
	if strings.Contains(plan.Steps[0].Description, "[tool:") {
		t.Errorf("step 1 description should not contain [tool:...], got: %s", plan.Steps[0].Description)
	}
}

// ---------------------------------------------------------------------------
// extractShellCommand
// ---------------------------------------------------------------------------

func TestExtractShellCommand(t *testing.T) {
	cases := []struct {
		desc string
		want string
	}{
		{"Run `ls -la /tmp`", "ls -la /tmp"},
		{"Execute `git status`", "git status"},
		{"List the files in the directory", ""},
		{"run cat README.md", ""}, // no backticks → not extracted
	}
	for _, tc := range cases {
		got := extractShellCommand(tc.desc)
		if got != tc.want {
			t.Errorf("extractShellCommand(%q) = %q, want %q", tc.desc, got, tc.want)
		}
	}
}
