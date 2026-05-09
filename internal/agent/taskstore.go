// Package agent — taskstore.go provides durable task tracking so long-running
// work survives Telegram timeouts and process restarts.
package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// DurableTask represents a long-running task that executes in the background.
type DurableTask struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Platform  string    `json:"platform"`
	ChatID    string    `json:"chat_id"`
	Task      string    `json:"task"`
	Status    string    `json:"status"` // queued, running, done, failed, cancelled
	Result    string    `json:"result,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	cancel context.CancelFunc // not serialised
}

// ToInfo converts a DurableTask to a serialisable TaskInfo.
func (t *DurableTask) ToInfo() core.TaskInfo {
	return core.TaskInfo{
		ID:        t.ID,
		Task:      t.Task,
		Status:    t.Status,
		Result:    t.Result,
		Error:     t.Error,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

// TaskStore provides durable, thread-safe task tracking with JSONL persistence.
type TaskStore struct {
	mu     sync.RWMutex
	tasks  map[string]*DurableTask
	path   string // JSONL file path
	nextID int
}

// NewTaskStore creates a store backed by the given JSONL file.
// Existing tasks are loaded from disk; any that were "running" when the
// process died are marked as "failed".
func NewTaskStore(path string) *TaskStore {
	s := &TaskStore{
		tasks:  make(map[string]*DurableTask),
		path:   path,
		nextID: 1,
	}
	s.loadFromDisk()
	return s
}

// Create adds a new queued task and persists it.
func (s *TaskStore) Create(userID, platform, chatID, task string) *DurableTask {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := fmt.Sprintf("task-%03d", s.nextID)
	s.nextID++
	now := time.Now().UTC()

	t := &DurableTask{
		ID:        id,
		UserID:    userID,
		Platform:  platform,
		ChatID:    chatID,
		Task:      task,
		Status:    "queued",
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.tasks[id] = t
	s.persistLocked()
	log.Info("task created", "id", id, "task", truncate(task, 60))
	return t
}

// Get retrieves a task by ID.
func (s *TaskStore) Get(id string) (*DurableTask, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	return t, ok
}

// List returns tasks for a given user (or all tasks if userID is empty),
// sorted by creation time descending (newest first).
func (s *TaskStore) List(userID string) []*DurableTask {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*DurableTask
	for _, t := range s.tasks {
		if userID == "" || t.UserID == userID {
			result = append(result, t)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

// Update sets the status and result/error for a task.
func (s *TaskStore) Update(id, status, result, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return
	}
	t.Status = status
	t.Result = result
	t.Error = errMsg
	t.UpdatedAt = time.Now().UTC()
	s.persistLocked()
}

// Cancel stops a running task.
func (s *TaskStore) Cancel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}
	if t.Status != "queued" && t.Status != "running" {
		return fmt.Errorf("task %q is already %s", id, t.Status)
	}

	if t.cancel != nil {
		t.cancel()
	}
	t.Status = "cancelled"
	t.UpdatedAt = time.Now().UTC()
	s.persistLocked()
	log.Info("task cancelled", "id", id)
	return nil
}

// SetCancel attaches a cancel function to a running task.
func (s *TaskStore) SetCancel(id string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tasks[id]; ok {
		t.cancel = cancel
	}
}

// persistLocked writes all tasks to the JSONL file. Caller must hold s.mu.
func (s *TaskStore) persistLocked() {
	if s.path == "" {
		return
	}
	f, err := os.Create(s.path)
	if err != nil {
		log.Warn("failed to persist tasks", "error", err)
		return
	}
	defer f.Close()

	for _, t := range s.tasks {
		data, err := json.Marshal(t)
		if err != nil {
			continue
		}
		f.Write(data)
		f.Write([]byte("\n"))
	}
}

// loadFromDisk reads tasks from the JSONL file. Tasks that were "running"
// when the process died are marked as "failed".
func (s *TaskStore) loadFromDisk() {
	if s.path == "" {
		return
	}
	f, err := os.Open(s.path)
	if err != nil {
		return // file doesn't exist yet
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var t DurableTask
		if err := json.Unmarshal(scanner.Bytes(), &t); err != nil {
			continue
		}
		// Mark orphaned running tasks as failed
		if t.Status == "running" || t.Status == "queued" {
			t.Status = "failed"
			t.Error = "process interrupted"
			t.UpdatedAt = time.Now().UTC()
		}
		s.tasks[t.ID] = &t
		// Track highest ID for next assignment
		var num int
		if _, err := fmt.Sscanf(t.ID, "task-%d", &num); err == nil && num >= s.nextID {
			s.nextID = num + 1
		}
	}

	if len(s.tasks) > 0 {
		log.Info("tasks loaded from disk", "count", len(s.tasks))
	}
}
