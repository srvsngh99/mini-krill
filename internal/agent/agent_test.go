package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
)

// ---------------------------------------------------------------------------
// Mock LLM provider
// ---------------------------------------------------------------------------

type MockProvider struct {
	chatResponse string
}

func (m *MockProvider) Chat(_ context.Context, msgs []core.Message, _ ...core.ChatOption) (*core.Response, error) {
	content := m.chatResponse
	if content == "" {
		content = "CHAT"
	}
	return &core.Response{
		Content:          content,
		Model:            "mock-model",
		PromptTokens:     10,
		CompletionTokens: 5,
	}, nil
}

func (m *MockProvider) Stream(_ context.Context, msgs []core.Message, _ ...core.ChatOption) (<-chan core.StreamChunk, error) {
	ch := make(chan core.StreamChunk, 1)
	content := m.chatResponse
	if content == "" {
		content = "mock stream chunk"
	}
	ch <- core.StreamChunk{Content: content, Done: true}
	close(ch)
	return ch, nil
}

func (m *MockProvider) Name() string                     { return "mock" }
func (m *MockProvider) ModelName() string                { return "mock-model" }
func (m *MockProvider) Available(_ context.Context) bool { return true }

// ---------------------------------------------------------------------------
// Mock Brain
// ---------------------------------------------------------------------------

type mockBrain struct{}

func (b *mockBrain) Memory() core.Memory               { return nil }
func (b *mockBrain) GetPersonality() *core.Personality  { return &core.Personality{Name: "TestKrill"} }
func (b *mockBrain) GetSoul() *core.Soul                { return &core.Soul{SystemPrompt: "You are a test krill.", Identity: "test"} }
func (b *mockBrain) SystemPrompt() string               { return "You are a test krill." }
func (b *mockBrain) RandomFact() string                 { return "Krill are tiny." }
func (b *mockBrain) EnrichMessages(msgs []core.Message) []core.Message {
	sysMsg := core.Message{Role: "system", Content: "You are a test krill."}
	if len(msgs) > 0 && msgs[0].Role == "system" {
		enriched := make([]core.Message, len(msgs))
		copy(enriched, msgs)
		enriched[0] = sysMsg
		return enriched
	}
	enriched := make([]core.Message, 0, len(msgs)+1)
	enriched = append(enriched, sysMsg)
	enriched = append(enriched, msgs...)
	return enriched
}

// ---------------------------------------------------------------------------
// Mock Skill Registry
// ---------------------------------------------------------------------------

type mockSkillRegistry struct{}

func (r *mockSkillRegistry) Register(_ core.Skill) error      { return nil }
func (r *mockSkillRegistry) Unregister(_ string) error         { return nil }
func (r *mockSkillRegistry) Get(_ string) (core.Skill, bool)   { return nil, false }
func (r *mockSkillRegistry) List() []core.SkillInfo             { return nil }

// ---------------------------------------------------------------------------
// Mock MCP Registry
// ---------------------------------------------------------------------------

type mockMCPReg struct{}

func (r *mockMCPReg) Register(_ string, _ core.MCPServer) error { return nil }
func (r *mockMCPReg) Get(_ string) (core.MCPServer, bool)       { return nil, false }
func (r *mockMCPReg) List() []core.MCPServerInfo                 { return nil }
func (r *mockMCPReg) AllTools() []core.MCPTool                   { return nil }
func (r *mockMCPReg) Close() error                               { return nil }

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func newTestAgent(response string) *KrillAgent {
	return New(
		config.AgentConfig{Name: "test-krill", MaxSubKrills: 3, PlanApproval: true},
		&MockProvider{chatResponse: response},
		&mockBrain{},
		&mockSkillRegistry{},
		&mockMCPReg{},
	)
}

// ---------------------------------------------------------------------------
// Agent tests
// ---------------------------------------------------------------------------

func TestNewAgent(t *testing.T) {
	a := newTestAgent("hello")
	if a == nil {
		t.Fatal("New returned nil")
	}
}

func TestAgentChat(t *testing.T) {
	// The mock always returns "Hello from the deep!" which doesn't contain TASK,
	// so classifyIntent defaults to CHAT, and the chat response is also this string.
	a := newTestAgent("Hello from the deep!")

	resp, err := a.Chat(context.Background(), "hi there")
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
	if resp == "" {
		t.Error("Chat() returned empty response")
	}
}

func TestAgentChatMultipleMessages(t *testing.T) {
	a := newTestAgent("response from krill")

	for i := 0; i < 5; i++ {
		resp, err := a.Chat(context.Background(), "message")
		if err != nil {
			t.Fatalf("Chat() iteration %d error: %v", i, err)
		}
		if resp == "" {
			t.Errorf("Chat() iteration %d returned empty response", i)
		}
	}
}

func TestAgentPlanApproval(t *testing.T) {
	// The mock LLM returns "CHAT" for classification (since "Just chatting"
	// doesn't contain "TASK"), so this tests the chat path.
	a := newTestAgent("Just chatting")

	resp, err := a.Chat(context.Background(), "build me a website")
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
	if resp == "" {
		t.Error("Chat() returned empty response for task-like message")
	}
}

func TestAgentPlanApprovalWithTaskClassification(t *testing.T) {
	// Use a provider that returns "TASK" for classification, then a plan response
	callCount := 0
	provider := &sequentialMockProvider{
		responses: []string{
			"TASK",
			"SUMMARY: Build a website\nSTEP 1: Set up\nSTEP 2: Code",
		},
		callCount: &callCount,
	}

	agent := New(
		config.AgentConfig{Name: "test-krill", MaxSubKrills: 3, PlanApproval: true},
		provider,
		&mockBrain{},
		&mockSkillRegistry{},
		&mockMCPReg{},
	)

	resp, err := agent.Chat(context.Background(), "build me a website")
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}

	if !strings.Contains(resp, "DIVE PLAN") {
		t.Errorf("expected plan output containing 'DIVE PLAN', got: %s", resp)
	}
	if !strings.Contains(resp, "Approve this plan") {
		t.Errorf("expected plan to ask for approval, got: %s", resp)
	}
}

func TestAgentApproveReject(t *testing.T) {
	a := newTestAgent("TASK")

	// Manually set a pending plan
	a.pendingPlan = &core.Plan{
		Task:    "test task",
		Summary: "test summary",
		Steps: []core.PlanStep{
			{ID: 1, Description: "step one", Status: "pending"},
		},
	}

	// Reject it
	resp, err := a.Chat(context.Background(), "no")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if resp == "" {
		t.Error("expected rejection response")
	}
	if a.pendingPlan != nil {
		t.Error("pending plan should be nil after rejection")
	}
}

func TestAgentApproveAndExecute(t *testing.T) {
	a := newTestAgent("executed result")

	// Manually set a pending plan
	a.pendingPlan = &core.Plan{
		Task:    "test task",
		Summary: "test summary",
		Steps: []core.PlanStep{
			{ID: 1, Description: "step one", Status: "pending"},
		},
	}

	// Approve it
	resp, err := a.Chat(context.Background(), "yes")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if resp == "" {
		t.Error("expected execution response after approval")
	}
	if a.pendingPlan != nil {
		t.Error("pending plan should be nil after approval and execution")
	}
}

// sequentialMockProvider returns different responses for sequential calls
type sequentialMockProvider struct {
	responses []string
	callCount *int
}

func (m *sequentialMockProvider) Chat(_ context.Context, _ []core.Message, _ ...core.ChatOption) (*core.Response, error) {
	idx := *m.callCount
	*m.callCount++
	content := "fallback"
	if idx < len(m.responses) {
		content = m.responses[idx]
	}
	return &core.Response{Content: content, Model: "mock"}, nil
}

func (m *sequentialMockProvider) Stream(_ context.Context, _ []core.Message, _ ...core.ChatOption) (<-chan core.StreamChunk, error) {
	ch := make(chan core.StreamChunk, 1)
	ch <- core.StreamChunk{Content: "stream", Done: true}
	close(ch)
	return ch, nil
}

func (m *sequentialMockProvider) Name() string                     { return "sequential-mock" }
func (m *sequentialMockProvider) ModelName() string                { return "sequential-mock" }
func (m *sequentialMockProvider) Available(_ context.Context) bool { return true }

// ---------------------------------------------------------------------------
// SubKrillManager tests
// ---------------------------------------------------------------------------

func TestSubKrillManagerSpawnAndWait(t *testing.T) {
	provider := &MockProvider{chatResponse: "sub-krill result"}
	cfg := config.AgentConfig{
		Name:         "test-krill",
		MaxSubKrills: 3,
	}

	mgr := NewSubKrillManager(cfg, provider)

	sub, err := mgr.Spawn(context.Background(), "analyze this data")
	if err != nil {
		t.Fatalf("Spawn() error: %v", err)
	}
	if sub == nil {
		t.Fatal("Spawn() returned nil")
	}
	if sub.ID == "" {
		t.Error("Spawn() returned SubKrill with empty ID")
	}
	if sub.Task != "analyze this data" {
		t.Errorf("SubKrill.Task = %q, want %q", sub.Task, "analyze this data")
	}

	// Wait for completion
	result, err := mgr.Wait(sub.ID, 5*time.Second)
	if err != nil {
		t.Fatalf("Wait() error: %v", err)
	}
	if result.Status != "done" {
		t.Errorf("SubKrill.Status = %q, want %q", result.Status, "done")
	}
	if result.Output == "" {
		t.Error("SubKrill.Output is empty after completion")
	}
}

func TestSubKrillManagerConcurrencyLimit(t *testing.T) {
	provider := &slowMockProvider{delay: 100 * time.Millisecond, response: "done"}
	cfg := config.AgentConfig{
		Name:         "test-krill",
		MaxSubKrills: 2,
	}

	mgr := NewSubKrillManager(cfg, provider)
	ctx := context.Background()

	// Spawn up to the limit
	sub1, err := mgr.Spawn(ctx, "task 1")
	if err != nil {
		t.Fatalf("Spawn(1) error: %v", err)
	}
	sub2, err := mgr.Spawn(ctx, "task 2")
	if err != nil {
		t.Fatalf("Spawn(2) error: %v", err)
	}

	// Third spawn should fail because we are at capacity
	_, err = mgr.Spawn(ctx, "task 3")
	if err == nil {
		t.Error("Spawn(3) should fail when at capacity, got nil error")
	}
	if err != nil && !strings.Contains(err.Error(), "capacity") {
		t.Errorf("error = %q, want to contain 'capacity'", err.Error())
	}

	// Wait for sub-krills to finish, then we should be able to spawn again
	_, _ = mgr.Wait(sub1.ID, 5*time.Second)
	_, _ = mgr.Wait(sub2.ID, 5*time.Second)

	// Cleanup completed sub-krills
	mgr.Cleanup()

	sub3, err := mgr.Spawn(ctx, "task 3 retry")
	if err != nil {
		t.Fatalf("Spawn(3 retry) after cleanup error: %v", err)
	}
	if sub3 == nil {
		t.Error("Spawn(3 retry) returned nil after cleanup")
	}
}

func TestSubKrillManagerList(t *testing.T) {
	provider := &MockProvider{chatResponse: "result"}
	cfg := config.AgentConfig{
		Name:         "test-krill",
		MaxSubKrills: 5,
	}

	mgr := NewSubKrillManager(cfg, provider)
	ctx := context.Background()

	_, _ = mgr.Spawn(ctx, "task a")
	_, _ = mgr.Spawn(ctx, "task b")

	// Wait briefly for goroutines to register
	time.Sleep(50 * time.Millisecond)

	list := mgr.List()
	if len(list) != 2 {
		t.Errorf("List() returned %d sub-krills, want 2", len(list))
	}
}

func TestSubKrillManagerWaitNotFound(t *testing.T) {
	provider := &MockProvider{chatResponse: "result"}
	cfg := config.AgentConfig{
		Name:         "test-krill",
		MaxSubKrills: 3,
	}

	mgr := NewSubKrillManager(cfg, provider)

	_, err := mgr.Wait("nonexistent-id", 1*time.Second)
	if err == nil {
		t.Error("Wait(nonexistent) should return error")
	}
}

func TestSubKrillManagerCleanup(t *testing.T) {
	provider := &MockProvider{chatResponse: "done"}
	cfg := config.AgentConfig{
		Name:         "test-krill",
		MaxSubKrills: 5,
	}

	mgr := NewSubKrillManager(cfg, provider)
	ctx := context.Background()

	sub, err := mgr.Spawn(ctx, "cleanup task")
	if err != nil {
		t.Fatalf("Spawn() error: %v", err)
	}

	// Wait for it to finish
	_, err = mgr.Wait(sub.ID, 5*time.Second)
	if err != nil {
		t.Fatalf("Wait() error: %v", err)
	}

	// Before cleanup, list should still have it
	if len(mgr.List()) != 1 {
		t.Errorf("List() before cleanup = %d, want 1", len(mgr.List()))
	}

	mgr.Cleanup()

	if len(mgr.List()) != 0 {
		t.Errorf("List() after cleanup = %d, want 0", len(mgr.List()))
	}
}

// slowMockProvider simulates a slow LLM for concurrency testing
type slowMockProvider struct {
	delay    time.Duration
	response string
}

func (m *slowMockProvider) Chat(_ context.Context, _ []core.Message, _ ...core.ChatOption) (*core.Response, error) {
	time.Sleep(m.delay)
	return &core.Response{Content: m.response, Model: "slow-mock"}, nil
}

func (m *slowMockProvider) Stream(_ context.Context, _ []core.Message, _ ...core.ChatOption) (<-chan core.StreamChunk, error) {
	ch := make(chan core.StreamChunk, 1)
	ch <- core.StreamChunk{Content: m.response, Done: true}
	close(ch)
	return ch, nil
}

func (m *slowMockProvider) Name() string                     { return "slow-mock" }
func (m *slowMockProvider) ModelName() string                { return "slow-mock" }
func (m *slowMockProvider) Available(_ context.Context) bool { return true }
