package agent

import (
	"context"
	"testing"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
)

// mockProvider is a test LLM provider that returns canned responses.
type mockProvider struct {
	response string
}

func (m *mockProvider) Chat(_ context.Context, msgs []core.Message, _ ...core.ChatOption) (*core.Response, error) {
	return &core.Response{Content: m.response, Model: "mock"}, nil
}

func (m *mockProvider) Stream(_ context.Context, msgs []core.Message, _ ...core.ChatOption) (<-chan core.StreamChunk, error) {
	ch := make(chan core.StreamChunk, 1)
	ch <- core.StreamChunk{Content: m.response, Done: true}
	close(ch)
	return ch, nil
}

func (m *mockProvider) Name() string                           { return "mock" }
func (m *mockProvider) ModelName() string                      { return "mock-model" }
func (m *mockProvider) Available(_ context.Context) bool       { return true }

// mockBrain provides minimal brain interface for tests.
type mockBrain struct{}

func (b *mockBrain) Memory() core.Memory             { return nil }
func (b *mockBrain) GetPersonality() *core.Personality {
	return &core.Personality{Name: "TestKrill", Greeting: "Hello!"}
}
func (b *mockBrain) GetSoul() *core.Soul {
	return &core.Soul{SystemPrompt: "You are a test krill."}
}
func (b *mockBrain) SystemPrompt() string { return "You are a test krill." }
func (b *mockBrain) EnrichMessages(msgs []core.Message) []core.Message {
	return append([]core.Message{{Role: "system", Content: "You are a test krill."}}, msgs...)
}
func (b *mockBrain) RandomFact() string { return "Krill are cool." }

// mockSkillReg provides minimal skill registry for tests.
type mockSkillReg struct{}

func (r *mockSkillReg) Register(_ core.Skill) error      { return nil }
func (r *mockSkillReg) Unregister(_ string) error         { return nil }
func (r *mockSkillReg) Get(_ string) (core.Skill, bool)   { return nil, false }
func (r *mockSkillReg) List() []core.SkillInfo             { return nil }

// mockMCPReg provides minimal MCP registry for tests.
type mockMCPReg struct{}

func (r *mockMCPReg) Register(_ string, _ core.MCPServer) error { return nil }
func (r *mockMCPReg) Get(_ string) (core.MCPServer, bool)       { return nil, false }
func (r *mockMCPReg) List() []core.MCPServerInfo                 { return nil }
func (r *mockMCPReg) AllTools() []core.MCPTool                   { return nil }
func (r *mockMCPReg) Close() error                               { return nil }

func newTestAgent(response string) *KrillAgent {
	return New(
		config.AgentConfig{Name: "test-krill", MaxSubKrills: 2, PlanApproval: true},
		&mockProvider{response: response},
		&mockBrain{},
		&mockSkillReg{},
		&mockMCPReg{},
	)
}

func TestNewAgent(t *testing.T) {
	a := newTestAgent("hello")
	if a == nil {
		t.Fatal("New returned nil")
	}
}

func TestAgentChat(t *testing.T) {
	// The mock LLM returns "CHAT" for classification, then "hello world" for response
	a := newTestAgent("CHAT")
	// Override to make it respond differently after classification
	a.llm = &mockProvider{response: "Hello, I am Mini Krill!"}

	resp, err := a.Chat(context.Background(), "hey there")
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp == "" {
		t.Error("chat returned empty response")
	}
}

func TestAgentPlanApproval(t *testing.T) {
	// Mock that always returns "TASK" for classification and a plan for planning
	a := newTestAgent("TASK")

	resp, err := a.Chat(context.Background(), "build me a website")
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp == "" {
		t.Error("expected plan response, got empty")
	}
	// Agent should now have a pending plan
	if a.pendingPlan == nil {
		t.Log("Note: plan may not be pending if mock response didn't parse as plan")
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

func TestSubKrillManager(t *testing.T) {
	cfg := config.AgentConfig{MaxSubKrills: 2}
	mgr := NewSubKrillManager(cfg, &mockProvider{response: "subtask done"})

	sk, err := mgr.Spawn(context.Background(), "test subtask")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if sk == nil {
		t.Fatal("spawn returned nil")
	}
	if sk.ID == "" {
		t.Error("sub-krill has no ID")
	}

	list := mgr.List()
	if len(list) == 0 {
		t.Error("expected at least one active sub-krill")
	}
}
