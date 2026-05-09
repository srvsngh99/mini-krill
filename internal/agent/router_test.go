package agent

import (
	"context"
	"testing"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
)

func newTestRouter(llmResponse string) *IntentRouter {
	return NewIntentRouter(
		&mockSkillRegistry{},
		&MockProvider{chatResponse: llmResponse},
	)
}

// ---------------------------------------------------------------------------
// Deterministic routing tests
// ---------------------------------------------------------------------------

func TestRouterDeterministicChat(t *testing.T) {
	r := newTestRouter("CHAT")
	cases := []string{
		"hi", "hello", "hey", "what's up",
		"good morning", "yo",
		"thanks", // short, no action verb
		"cool",   // short, no action verb
	}
	for _, input := range cases {
		result := r.Classify(context.Background(), input)
		if result.Intent != IntentChat {
			t.Errorf("Classify(%q) = %s, want CHAT", input, result.Intent)
		}
	}
}

func TestRouterDeterministicRememberStore(t *testing.T) {
	r := newTestRouter("CHAT")
	cases := []string{
		"remember that I prefer Go",
		"learn that my name is Sourav",
		"memorize this: I like dark mode",
		"note that I use vim",
	}
	for _, input := range cases {
		result := r.Classify(context.Background(), input)
		if result.Intent != IntentRemember {
			t.Errorf("Classify(%q) = %s, want REMEMBER", input, result.Intent)
		}
		if result.ToolName != "store" {
			t.Errorf("Classify(%q).ToolName = %q, want 'store'", input, result.ToolName)
		}
	}
}

func TestRouterDeterministicRememberRecall(t *testing.T) {
	r := newTestRouter("CHAT")
	cases := []string{
		"what do you remember about me",
		"do you remember our last conversation",
		"what have you learned",
		"we talked about this last time",
	}
	for _, input := range cases {
		result := r.Classify(context.Background(), input)
		if result.Intent != IntentRemember && result.Intent != IntentSelfSkill {
			t.Errorf("Classify(%q) = %s, want REMEMBER or SELF_SKILL", input, result.Intent)
		}
	}
}

func TestRouterDeterministicRememberForget(t *testing.T) {
	r := newTestRouter("CHAT")
	cases := []string{
		"forget that I like Python",
		"forget about the old project",
		"delete memory about coffee",
	}
	for _, input := range cases {
		result := r.Classify(context.Background(), input)
		if result.Intent != IntentRemember {
			t.Errorf("Classify(%q) = %s, want REMEMBER", input, result.Intent)
		}
		if result.ToolName != "forget" {
			t.Errorf("Classify(%q).ToolName = %q, want 'forget'", input, result.ToolName)
		}
	}
}

func TestRouterDeterministicToolTaskTime(t *testing.T) {
	r := newTestRouter("CHAT")
	cases := []string{
		"what time is it",
		"what's the date today",
		"what day is it",
	}
	for _, input := range cases {
		result := r.Classify(context.Background(), input)
		if result.Intent != IntentToolTask {
			t.Errorf("Classify(%q) = %s, want TOOL_TASK", input, result.Intent)
		}
		if result.ToolName != "time" {
			t.Errorf("Classify(%q).ToolName = %q, want 'time'", input, result.ToolName)
		}
	}
}

func TestRouterDeterministicToolTaskSearch(t *testing.T) {
	r := newTestRouter("CHAT")
	cases := []string{
		"search for Go testing best practices",
		"google kubernetes deployment",
		"look up rust async patterns",
	}
	for _, input := range cases {
		result := r.Classify(context.Background(), input)
		if result.Intent != IntentToolTask {
			t.Errorf("Classify(%q) = %s, want TOOL_TASK", input, result.Intent)
		}
		if result.ToolName != "search" {
			t.Errorf("Classify(%q).ToolName = %q, want 'search'", input, result.ToolName)
		}
	}
}

func TestRouterDeterministicToolTaskSysInfo(t *testing.T) {
	r := newTestRouter("CHAT")
	cases := []string{
		"system info",
		"how much ram do I have",
		"what os am I running",
	}
	for _, input := range cases {
		result := r.Classify(context.Background(), input)
		if result.Intent != IntentToolTask {
			t.Errorf("Classify(%q) = %s, want TOOL_TASK", input, result.Intent)
		}
		if result.ToolName != "sysinfo" {
			t.Errorf("Classify(%q).ToolName = %q, want 'sysinfo'", input, result.ToolName)
		}
	}
}

func TestRouterDeterministicToolTaskYouTube(t *testing.T) {
	r := newTestRouter("CHAT")
	input := "summarize https://youtube.com/watch?v=abc123"
	result := r.Classify(context.Background(), input)
	if result.Intent != IntentToolTask {
		t.Errorf("Classify(youtube URL) = %s, want TOOL_TASK", result.Intent)
	}
	if result.ToolName != "youtube" {
		t.Errorf("Classify(youtube URL).ToolName = %q, want 'youtube'", result.ToolName)
	}
}

func TestRouterDeterministicToolTaskWeb(t *testing.T) {
	r := newTestRouter("CHAT")
	input := "read https://example.com/article"
	result := r.Classify(context.Background(), input)
	if result.Intent != IntentToolTask {
		t.Errorf("Classify(web URL) = %s, want TOOL_TASK", result.Intent)
	}
	if result.ToolName != "web" {
		t.Errorf("Classify(web URL).ToolName = %q, want 'web'", result.ToolName)
	}
}

func TestRouterDeterministicSelfSkill(t *testing.T) {
	r := newTestRouter("CHAT")
	cases := []struct {
		input string
		skill string
	}{
		{"what can you do", "self:skills"},
		{"your personality", "self:inspect"},
		{"your status", "self:status"},
		{"your health", "self:health"},
		{"your config", "self:config"},
		{"evolve your personality", "self:evolve"},
		{"heal yourself", "self:heal"},
	}
	for _, tc := range cases {
		result := r.Classify(context.Background(), tc.input)
		if result.Intent != IntentSelfSkill {
			t.Errorf("Classify(%q) = %s, want SELF_SKILL", tc.input, result.Intent)
		}
		if result.SkillName != tc.skill {
			t.Errorf("Classify(%q).SkillName = %q, want %q", tc.input, result.SkillName, tc.skill)
		}
	}
}

func TestRouterDeterministicDiagnose(t *testing.T) {
	r := newTestRouter("CHAT")
	cases := []string{
		"why did I get this error?",
		"why is my server crashing?",
		"what went wrong with the deploy",
		"I got a connection refused error, what does it mean?",
		"why am i getting a 404?",
	}
	for _, input := range cases {
		result := r.Classify(context.Background(), input)
		if result.Intent != IntentDiagnose {
			t.Errorf("Classify(%q) = %s, want DIAGNOSE", input, result.Intent)
		}
	}
}

func TestRouterDeterministicCommand(t *testing.T) {
	r := newTestRouter("CHAT")
	cases := []string{
		"/model",
		"/models",
		"/use codex",
		"/auth claude",
		"/tasks",
		"/task 001",
		"/cancel 001",
	}
	for _, input := range cases {
		result := r.Classify(context.Background(), input)
		if result.Intent != IntentCommand {
			t.Errorf("Classify(%q) = %s, want COMMAND", input, result.Intent)
		}
	}
}

func TestRouterCommandDoesNotOvermatch(t *testing.T) {
	r := newTestRouter("ANSWER")
	// "/help me debug this" should NOT match /help — it's a question
	result := r.Classify(context.Background(), "/help me debug this")
	if result.Intent == IntentCommand {
		t.Error("Classify('/help me debug this') should NOT match COMMAND")
	}
}

// ---------------------------------------------------------------------------
// LLM fallback tests
// ---------------------------------------------------------------------------

func TestRouterLLMFallbackLongTask(t *testing.T) {
	// Ambiguous input that doesn't match any deterministic pattern
	r := newTestRouter("LONG_TASK")
	result := r.Classify(context.Background(), "please refactor the authentication module thoroughly")
	if result.Intent != IntentLongTask {
		t.Errorf("Classify(ambiguous) = %s, want LONG_TASK", result.Intent)
	}
}

func TestRouterLLMFallbackAnswer(t *testing.T) {
	r := newTestRouter("ANSWER")
	result := r.Classify(context.Background(), "how does garbage collection work in Go")
	if result.Intent != IntentAnswer {
		t.Errorf("Classify(question) = %s, want ANSWER", result.Intent)
	}
}

func TestRouterLLMFallbackLegacyTask(t *testing.T) {
	// Old-style "TASK" response should map to LONG_TASK for backward compat
	r := newTestRouter("TASK")
	result := r.Classify(context.Background(), "please refactor the authentication module thoroughly")
	if result.Intent != IntentLongTask {
		t.Errorf("Classify(legacy TASK) = %s, want LONG_TASK", result.Intent)
	}
}

func TestRouterLLMFailureDefaultsToAnswer(t *testing.T) {
	// When LLM returns gibberish, default to ANSWER (safe, still helpful)
	r := newTestRouter("blah blah nonsense")
	result := r.Classify(context.Background(), "please refactor the authentication module thoroughly")
	if result.Intent != IntentAnswer {
		t.Errorf("Classify(LLM gibberish) = %s, want ANSWER", result.Intent)
	}
}

// ---------------------------------------------------------------------------
// Auto-execute vs approval tests
// ---------------------------------------------------------------------------

func TestShouldRequireApprovalAlways(t *testing.T) {
	a := New(
		config.AgentConfig{Name: "test", PlanApproval: "always"},
		&MockProvider{chatResponse: "CHAT"},
		&mockBrain{},
		&mockSkillRegistry{},
		&mockMCPReg{},
	)

	plan := &core.Plan{
		Task:  "small task",
		Steps: []core.PlanStep{{ID: 1, Description: "do something"}},
	}

	if !a.shouldRequireApproval(plan) {
		t.Error("PlanApproval=always should always require approval")
	}
}

func TestShouldRequireApprovalNever(t *testing.T) {
	a := New(
		config.AgentConfig{Name: "test", PlanApproval: "never"},
		&MockProvider{chatResponse: "CHAT"},
		&mockBrain{},
		&mockSkillRegistry{},
		&mockMCPReg{},
	)

	plan := &core.Plan{
		Task: "dangerous task",
		Steps: []core.PlanStep{
			{ID: 1, Description: "delete everything"},
			{ID: 2, Description: "deploy to prod"},
			{ID: 3, Description: "remove all data"},
			{ID: 4, Description: "push to main"},
			{ID: 5, Description: "reset database"},
		},
	}

	if a.shouldRequireApproval(plan) {
		t.Error("PlanApproval=never should never require approval")
	}
}

func TestShouldRequireApprovalAutoSmallSafe(t *testing.T) {
	a := New(
		config.AgentConfig{Name: "test", PlanApproval: "auto"},
		&MockProvider{chatResponse: "CHAT"},
		&mockBrain{},
		&mockSkillRegistry{},
		&mockMCPReg{},
	)

	// 3 steps, no destructive operations
	plan := &core.Plan{
		Task: "check the repo",
		Steps: []core.PlanStep{
			{ID: 1, Description: "list files"},
			{ID: 2, Description: "read README"},
			{ID: 3, Description: "summarize findings"},
		},
	}

	if a.shouldRequireApproval(plan) {
		t.Error("auto mode: 3-step non-destructive plan should NOT require approval")
	}
}

func TestShouldRequireApprovalAutoLargePlan(t *testing.T) {
	a := New(
		config.AgentConfig{Name: "test", PlanApproval: "auto"},
		&MockProvider{chatResponse: "CHAT"},
		&mockBrain{},
		&mockSkillRegistry{},
		&mockMCPReg{},
	)

	// 6 steps
	plan := &core.Plan{
		Task: "refactor auth",
		Steps: []core.PlanStep{
			{ID: 1, Description: "analyze code"},
			{ID: 2, Description: "plan changes"},
			{ID: 3, Description: "write new module"},
			{ID: 4, Description: "update tests"},
			{ID: 5, Description: "run tests"},
			{ID: 6, Description: "document changes"},
		},
	}

	if !a.shouldRequireApproval(plan) {
		t.Error("auto mode: 6-step plan should require approval")
	}
}

func TestShouldRequireApprovalAutoDestructiveStep(t *testing.T) {
	a := New(
		config.AgentConfig{Name: "test", PlanApproval: "auto"},
		&MockProvider{chatResponse: "CHAT"},
		&mockBrain{},
		&mockSkillRegistry{},
		&mockMCPReg{},
	)

	// 2 steps but one is destructive
	plan := &core.Plan{
		Task: "clean up",
		Steps: []core.PlanStep{
			{ID: 1, Description: "find old files"},
			{ID: 2, Description: "delete file unused_module.go"},
		},
	}

	if !a.shouldRequireApproval(plan) {
		t.Error("auto mode: plan with 'delete' step should require approval")
	}
}

func TestShouldRequireApprovalAutoNoFalsePositives(t *testing.T) {
	a := New(
		config.AgentConfig{Name: "test", PlanApproval: "auto"},
		&MockProvider{chatResponse: "CHAT"},
		&mockBrain{},
		&mockSkillRegistry{},
		&mockMCPReg{},
	)

	// Benign steps containing substrings of danger words should NOT trigger approval
	plan := &core.Plan{
		Task: "build a dropdown menu",
		Steps: []core.PlanStep{
			{ID: 1, Description: "create a drop-down component"},
			{ID: 2, Description: "add push notification support"},
			{ID: 3, Description: "reset filter state on close"},
		},
	}

	if a.shouldRequireApproval(plan) {
		t.Error("auto mode: plan with 'drop-down', 'push notification', 'reset filter' should NOT require approval (word boundary matching)")
	}
}

func TestShouldRequireApprovalAutoVagueTask(t *testing.T) {
	a := New(
		config.AgentConfig{Name: "test", PlanApproval: "auto"},
		&MockProvider{chatResponse: "CHAT"},
		&mockBrain{},
		&mockSkillRegistry{},
		&mockMCPReg{},
	)

	// Vague task
	plan := &core.Plan{
		Task: "improve everything in the whole project",
		Steps: []core.PlanStep{
			{ID: 1, Description: "analyze code"},
			{ID: 2, Description: "make improvements"},
		},
	}

	if !a.shouldRequireApproval(plan) {
		t.Error("auto mode: vague task should require approval")
	}
}

// ---------------------------------------------------------------------------
// detectSelfSkillTrigger
// ---------------------------------------------------------------------------

func TestDetectSelfSkillTrigger(t *testing.T) {
	cases := []struct {
		input string
		skill string
	}{
		{"what can you do", "self:skills"},
		{"who are you", "self:inspect"},
		{"check yourself", "self:health"},
		{"your uptime", "self:status"},
		{"show config", "self:config"},
		{"no match here", ""},
	}

	for _, tc := range cases {
		skill, _ := detectSelfSkillTrigger(tc.input)
		if skill != tc.skill {
			t.Errorf("detectSelfSkillTrigger(%q) = %q, want %q", tc.input, skill, tc.skill)
		}
	}
}
