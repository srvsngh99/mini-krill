package reminder

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreAddListDueAndDone(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "reminders.jsonl"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	now := time.Now()
	r, err := store.Add("test", now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	due, err := store.Due(now)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	if len(due) != 1 || due[0].ID != r.ID {
		t.Fatalf("due = %+v, want reminder %s", due, r.ID)
	}
	if err := store.MarkDone(r.ID); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}
	due, _ = store.Due(now)
	if len(due) != 0 {
		t.Fatalf("expected no due reminders after done, got %+v", due)
	}
}

func TestParseRelativeReminder(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	text, due, err := Parse("call mom in 10 minutes", "", now)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if text != "call mom" {
		t.Fatalf("text = %q", text)
	}
	if !due.Equal(now.Add(10 * time.Minute)) {
		t.Fatalf("due = %s", due)
	}
}
