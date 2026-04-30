package chat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/srvsngh99/mini-krill/internal/core"
	"github.com/srvsngh99/mini-krill/internal/reminder"
)

type fakeAgent struct{}

func (fakeAgent) Chat(context.Context, string) (string, error)               { return "agent response", nil }
func (fakeAgent) Plan(context.Context, string) (*core.Plan, error)           { return nil, nil }
func (fakeAgent) ExecutePlan(context.Context, *core.Plan) (string, error)    { return "", nil }
func (fakeAgent) SpawnKrill(context.Context, string) (*core.SubKrill, error) { return nil, nil }

func TestCleanInternalPrefixes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no prefix",
			input: "Hello! How can I help?",
			want:  "Hello! How can I help?",
		},
		{
			name:  "plan prefix with boundary",
			input: "Plan: greet the user. Hi, Mini Krill online. What are we navigating?",
			want:  "Hi, Mini Krill online. What are we navigating?",
		},
		{
			name:  "thinking prefix with boundary",
			input: "Thinking: the user said hi. Hey there! What's up?",
			want:  "Hey there! What's up?",
		},
		{
			name:  "internal prefix case insensitive",
			input: "PLAN: respond casually. Yo, what's good?",
			want:  "Yo, what's good?",
		},
		{
			name:  "prefix without sentence boundary returns original",
			input: "Plan: this has no end",
			want:  "Plan: this has no end",
		},
		{
			name:  "entire response is preamble returns original",
			input: "Plan: greet the user.",
			want:  "Plan: greet the user.",
		},
		{
			name:  "legitimate content starting with plan word",
			input: "Plan your project with these 5 steps...",
			want:  "Plan your project with these 5 steps...",
		},
		{
			name:  "plan colon without space is not a prefix",
			input: "Plan:B is ready to go.",
			want:  "Plan:B is ready to go.",
		},
		{
			name:  "exclamation boundary",
			input: "Plan: great idea! Here is what I think.",
			want:  "Here is what I think.",
		},
		{
			name:  "mixed case prefix",
			input: "thinking: user wants help. Sure, I can help you.",
			want:  "Sure, I can help you.",
		},
		{
			name:  "preserves whitespace-only input",
			input: "   ",
			want:  "   ",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanInternalPrefixes(tt.input)
			if got != tt.want {
				t.Errorf("cleanInternalPrefixes(%q)\n  got:  %q\n  want: %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestHandleMessageCreatesReminder(t *testing.T) {
	store, err := reminder.NewStore(filepath.Join(t.TempDir(), "reminders.jsonl"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	handler := NewHandler(fakeAgent{}, store)
	resp, err := handler.HandleMessage(context.Background(), core.ChatMessage{
		Platform: "test",
		Text:     "remind me to check build in 5 minutes",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if !strings.Contains(resp, "Reminder") {
		t.Fatalf("expected reminder response, got %q", resp)
	}
	items, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].Text != "check build" {
		t.Fatalf("items = %+v", items)
	}
}
