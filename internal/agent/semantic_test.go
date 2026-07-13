package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
)

// fakeSemantic is a stand-in for the colony's vector store: it records what was
// remembered and returns a fixed recall block.
type fakeSemantic struct {
	mu         sync.Mutex
	block      string
	queries    []string
	remembered []string
	fail       bool // simulate an unreachable embedder / vector service
}

func (f *fakeSemantic) RecallBlock(_ context.Context, query string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, query)
	if f.fail {
		return ""
	}
	return f.block
}

func (f *fakeSemantic) Remember(_ context.Context, id, text string, meta map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.remembered = append(f.remembered, id+"|"+text+"|"+meta["kind"].(string))
}

func (f *fakeSemantic) snapshot() ([]string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.queries...), append([]string(nil), f.remembered...)
}

// capturingProvider records the messages of the last Chat call so a test can
// assert what actually went into the prompt.
type capturingProvider struct {
	MockProvider
	mu   sync.Mutex
	last []core.Message
}

func (c *capturingProvider) Chat(ctx context.Context, msgs []core.Message, opts ...core.ChatOption) (*core.Response, error) {
	c.mu.Lock()
	c.last = append([]core.Message(nil), msgs...)
	c.mu.Unlock()
	return c.MockProvider.Chat(ctx, msgs, opts...)
}

func (c *capturingProvider) lastMessages() []core.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]core.Message(nil), c.last...)
}

func newSemanticTestAgent(response string) (*KrillAgent, *capturingProvider) {
	p := &capturingProvider{MockProvider: MockProvider{chatResponse: response}}
	a := New(
		config.AgentConfig{Name: "test-krill", MaxSubKrills: 3, PlanApproval: "always"},
		p,
		&mockBrain{},
		&mockSkillRegistry{},
		&mockMCPReg{},
	)
	return a, p
}

// The chat path (not just the feed watcher) must inject the semantic recall
// block, otherwise the agent has no meaning-based memory of DMs at all.
func TestChatPromptCarriesSemanticRecall(t *testing.T) {
	a, prov := newSemanticTestAgent("Sure thing.")
	mem := &fakeSemantic{block: "YOUR RELEVANT MEMORY (things you said or heard before - reference them naturally if useful, don't repeat yourself):\n- yesterday - [dm_in] the owner asked about the map renderer\n"}
	a.SetSemanticMemory(mem)

	if _, err := a.ChatFromPlatform(context.Background(), "telegram", "42", "how is the map renderer going?"); err != nil {
		t.Fatalf("ChatFromPlatform: %v", err)
	}

	var found bool
	for _, m := range prov.lastMessages() {
		if m.Role == "system" && strings.Contains(m.Content, "YOUR RELEVANT MEMORY") {
			found = true
		}
	}
	if !found {
		t.Fatal("chat prompt is missing the semantic recall block")
	}

	queries, _ := mem.snapshot()
	if len(queries) == 0 || queries[0] != "how is the map renderer going?" {
		t.Fatalf("recall keyed on the wrong query: %v", queries)
	}
}

// Both sides of a chat turn must land in semantic memory, otherwise recall has
// nothing to find in a later conversation.
func TestChatTurnIsRemembered(t *testing.T) {
	a, _ := newSemanticTestAgent("The renderer is fine.")
	mem := &fakeSemantic{}
	a.SetSemanticMemory(mem)

	if _, err := a.ChatFromPlatform(context.Background(), "telegram", "42", "how is the map renderer going?"); err != nil {
		t.Fatalf("ChatFromPlatform: %v", err)
	}

	// Writes are async (they must never block a reply), so wait for both.
	var stored []string
	for i := 0; i < 200; i++ {
		if _, stored = mem.snapshot(); len(stored) >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(stored) < 2 {
		t.Fatalf("expected the user turn and the reply to be remembered, got %v", stored)
	}
	var in, out bool
	for _, s := range stored {
		if strings.HasSuffix(s, "|dm_in") && strings.Contains(s, "how is the map renderer going?") {
			in = true
		}
		if strings.HasSuffix(s, "|dm_out") {
			out = true
		}
	}
	if !in || !out {
		t.Fatalf("expected a dm_in and a dm_out memory, got %v", stored)
	}
}

// An unreachable embedder / vector service must not break a chat turn: the
// agent still replies, just without a recall block.
func TestChatSurvivesSemanticMemoryFailure(t *testing.T) {
	a, prov := newSemanticTestAgent("Still talking.")
	a.SetSemanticMemory(&fakeSemantic{fail: true})

	resp, err := a.ChatFromPlatform(context.Background(), "telegram", "42", "hello")
	if err != nil {
		t.Fatalf("ChatFromPlatform: %v", err)
	}
	if resp == "" {
		t.Fatal("agent must still reply when semantic memory is unreachable")
	}
	for _, m := range prov.lastMessages() {
		if strings.Contains(m.Content, "YOUR RELEVANT MEMORY") {
			t.Fatal("empty recall must not be injected")
		}
	}
}

// The public (non-colony) build never calls SetSemanticMemory, so the store is
// nil. That must be a no-op, not a panic.
func TestChatWithoutSemanticMemory(t *testing.T) {
	a, _ := newSemanticTestAgent("Public build reply.")
	if _, err := a.ChatFromPlatform(context.Background(), "cli", "", "hello"); err != nil {
		t.Fatalf("ChatFromPlatform with nil semantic memory: %v", err)
	}
}
