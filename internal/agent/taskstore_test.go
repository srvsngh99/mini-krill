package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTaskStoreCreateAndGet(t *testing.T) {
	store := NewTaskStore("")

	dt := store.Create("user1", "telegram", "chat123", "analyze the repo")
	if dt == nil {
		t.Fatal("Create returned nil")
	}
	if dt.ID == "" {
		t.Error("task ID is empty")
	}
	if dt.Status != "queued" {
		t.Errorf("status = %q, want queued", dt.Status)
	}
	if dt.Task != "analyze the repo" {
		t.Errorf("task = %q, want 'analyze the repo'", dt.Task)
	}

	got, ok := store.Get(dt.ID)
	if !ok {
		t.Fatal("Get returned false for just-created task")
	}
	if got.ID != dt.ID {
		t.Errorf("Get ID = %q, want %q", got.ID, dt.ID)
	}
}

func TestTaskStoreGetNotFound(t *testing.T) {
	store := NewTaskStore("")
	_, ok := store.Get("nonexistent")
	if ok {
		t.Error("Get(nonexistent) should return false")
	}
}

func TestTaskStoreList(t *testing.T) {
	store := NewTaskStore("")
	store.Create("user1", "telegram", "c1", "task A")
	store.Create("user2", "telegram", "c2", "task B")
	store.Create("user1", "telegram", "c1", "task C")

	// All tasks
	all := store.List("")
	if len(all) != 3 {
		t.Errorf("List('') = %d tasks, want 3", len(all))
	}

	// Filter by user
	u1 := store.List("user1")
	if len(u1) != 2 {
		t.Errorf("List(user1) = %d tasks, want 2", len(u1))
	}

	// Verify sorted by newest first
	if len(u1) >= 2 && u1[0].CreatedAt.Before(u1[1].CreatedAt) {
		t.Error("List should return newest first")
	}
}

func TestTaskStoreUpdate(t *testing.T) {
	store := NewTaskStore("")
	dt := store.Create("user1", "tg", "c1", "test task")

	store.Update(dt.ID, "done", "result text", "")

	got, ok := store.Get(dt.ID)
	if !ok {
		t.Fatal("Get failed after update")
	}
	if got.Status != "done" {
		t.Errorf("status = %q, want done", got.Status)
	}
	if got.Result != "result text" {
		t.Errorf("result = %q, want 'result text'", got.Result)
	}
}

func TestTaskStoreCancel(t *testing.T) {
	store := NewTaskStore("")
	dt := store.Create("user1", "tg", "c1", "cancellable task")

	err := store.Cancel(dt.ID)
	if err != nil {
		t.Fatalf("Cancel error: %v", err)
	}

	got, _ := store.Get(dt.ID)
	if got.Status != "cancelled" {
		t.Errorf("status = %q, want cancelled", got.Status)
	}
}

func TestTaskStoreCancelAlreadyDone(t *testing.T) {
	store := NewTaskStore("")
	dt := store.Create("user1", "tg", "c1", "done task")
	store.Update(dt.ID, "done", "result", "")

	err := store.Cancel(dt.ID)
	if err == nil {
		t.Error("cancelling a done task should return error")
	}
}

func TestTaskStoreCancelNotFound(t *testing.T) {
	store := NewTaskStore("")
	err := store.Cancel("nonexistent")
	if err == nil {
		t.Error("cancelling nonexistent task should return error")
	}
}

func TestTaskStorePersistAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.jsonl")

	// Create tasks and persist
	store1 := NewTaskStore(path)
	dt1 := store1.Create("user1", "tg", "c1", "task one")
	store1.Update(dt1.ID, "done", "result one", "")
	dt2 := store1.Create("user1", "tg", "c1", "task two")
	_ = dt2 // still queued

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("tasks file not created: %v", err)
	}

	// Load from disk in a new store
	store2 := NewTaskStore(path)

	// Task 1 (was done) should still be done
	got1, ok := store2.Get(dt1.ID)
	if !ok {
		t.Fatal("task one not found after reload")
	}
	if got1.Status != "done" {
		t.Errorf("task one status = %q, want done", got1.Status)
	}

	// Task 2 (was queued when "crashed") should be marked failed
	got2, ok := store2.Get(dt2.ID)
	if !ok {
		t.Fatal("task two not found after reload")
	}
	if got2.Status != "failed" {
		t.Errorf("task two status = %q, want failed (orphaned)", got2.Status)
	}
	if got2.Error != "process interrupted" {
		t.Errorf("task two error = %q, want 'process interrupted'", got2.Error)
	}
}

func TestTaskStoreNextIDAfterReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.jsonl")

	store1 := NewTaskStore(path)
	store1.Create("u", "tg", "c", "a")
	store1.Create("u", "tg", "c", "b")
	// nextID should be 3

	store2 := NewTaskStore(path)
	dt := store2.Create("u", "tg", "c", "c")
	if dt.ID != "task-003" {
		t.Errorf("ID after reload = %q, want task-003", dt.ID)
	}
}

func TestTaskStoreToInfo(t *testing.T) {
	store := NewTaskStore("")
	dt := store.Create("u1", "tg", "c1", "test")
	store.Update(dt.ID, "done", "result", "")

	got, _ := store.Get(dt.ID)
	info := got.ToInfo()
	if info.ID != dt.ID {
		t.Errorf("ToInfo().ID = %q, want %q", info.ID, dt.ID)
	}
	if info.Status != "done" {
		t.Errorf("ToInfo().Status = %q, want done", info.Status)
	}
	if info.Result != "result" {
		t.Errorf("ToInfo().Result = %q, want 'result'", info.Result)
	}
}
