package reminder

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Reminder struct {
	ID        string     `json:"id"`
	Text      string     `json:"text"`
	DueAt     time.Time  `json:"due_at"`
	CreatedAt time.Time  `json:"created_at"`
	FiredAt   *time.Time `json:"fired_at,omitempty"`
	DoneAt    *time.Time `json:"done_at,omitempty"`
}

type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("reminder store path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create reminder dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("open reminder store: %w", err)
	}
	_ = f.Close()
	return &Store{path: path}, nil
}

func (s *Store) Add(text string, dueAt time.Time) (*Reminder, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("reminder text is empty")
	}
	if dueAt.IsZero() {
		return nil, fmt.Errorf("due time is empty")
	}
	r := Reminder{
		ID:        fmt.Sprintf("r%d", time.Now().UnixNano()),
		Text:      text,
		DueAt:     dueAt.UTC(),
		CreatedAt: time.Now().UTC(),
	}
	if err := s.append(r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) List() ([]Reminder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadAllLocked()
}

func (s *Store) Due(now time.Time) ([]Reminder, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	var due []Reminder
	for _, r := range all {
		if r.DoneAt == nil && r.FiredAt == nil && !r.DueAt.After(now.UTC()) {
			due = append(due, r)
		}
	}
	return due, nil
}

func (s *Store) MarkDone(id string) error {
	return s.update(id, func(r *Reminder) {
		now := time.Now().UTC()
		r.DoneAt = &now
	})
}

func (s *Store) MarkFired(id string) error {
	return s.update(id, func(r *Reminder) {
		now := time.Now().UTC()
		r.FiredAt = &now
	})
}

func (s *Store) Delete(id string) error {
	return s.update(id, func(r *Reminder) {
		now := time.Now().UTC()
		r.DoneAt = &now
		r.FiredAt = &now
	})
}

func (s *Store) append(r Reminder) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open reminder store: %w", err)
	}
	defer f.Close()
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal reminder: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write reminder: %w", err)
	}
	return nil
}

func (s *Store) update(id string, fn func(*Reminder)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.loadAllLocked()
	if err != nil {
		return err
	}
	found := false
	for i := range all {
		if all[i].ID == id {
			fn(&all[i])
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("reminder %s not found", id)
	}
	return s.rewriteLocked(all)
}

func (s *Store) loadAllLocked() ([]Reminder, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open reminder store: %w", err)
	}
	defer f.Close()
	var reminders []Reminder
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var r Reminder
		if err := json.Unmarshal(scanner.Bytes(), &r); err == nil && r.ID != "" {
			reminders = append(reminders, r)
		}
	}
	return reminders, scanner.Err()
}

func (s *Store) rewriteLocked(reminders []Reminder) error {
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open reminder temp store: %w", err)
	}
	for _, r := range reminders {
		data, err := json.Marshal(r)
		if err != nil {
			_ = f.Close()
			return fmt.Errorf("marshal reminder: %w", err)
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			_ = f.Close()
			return fmt.Errorf("write reminder: %w", err)
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func StartScheduler(ctx context.Context, store *Store, interval time.Duration, notify func(Reminder)) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	check := func() {
		due, err := store.Due(time.Now())
		if err != nil {
			return
		}
		for _, r := range due {
			if notify != nil {
				notify(r)
			}
			_ = store.MarkFired(r.ID)
		}
	}
	check()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}
