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
