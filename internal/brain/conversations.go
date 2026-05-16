package brain

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// ConversationStore persists conversation turns to an append-only JSONL file.
// JSONL keeps release builds fully static and avoids CGO-dependent SQLite.
type ConversationStore struct {
	path         string
	maxFileSize  int64 // rotate when file exceeds this size (default 5MB)
	maxKeepTurns int   // keep this many recent turns after rotation (default 500)
	mu           sync.Mutex
}

type conversationTurn struct {
	Channel   string    `json:"channel"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// NewConversationStore opens (or creates) a durable JSONL conversation store.
func NewConversationStore(path string) (*ConversationStore, error) {
	if path == "" {
		return nil, fmt.Errorf("conversation store path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create conversation dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("open conversations store: %w", err)
	}
	_ = f.Close()

	store := &ConversationStore{
		path:         path,
		maxFileSize:  5 * 1024 * 1024, // 5MB
		maxKeepTurns: 500,
	}
	count, _ := store.countTurns()
	log.Info("conversation store initialized", "path", path, "turns", count)
	return store, nil
}

// SaveTurn writes a single user or assistant message to durable storage.
func (s *ConversationStore) SaveTurn(channel, role, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open conversations store: %w", err)
	}
	defer f.Close()

	turn := conversationTurn{
		Channel:   channel,
		Role:      role,
		Content:   content,
		Timestamp: time.Now().UTC(),
	}
	data, err := json.Marshal(turn)
	if err != nil {
		return fmt.Errorf("marshal turn: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("save turn: %w", err)
	}

	// Rotate if file exceeds size threshold
	if info, err := os.Stat(s.path); err == nil && info.Size() > s.maxFileSize {
		s.rotateLocked()
	}
	return nil
}

// LoadRecent returns the last n turns for the given channel, ordered oldest-first.
func (s *ConversationStore) LoadRecent(channel string, n int) ([]core.Message, error) {
	if n <= 0 {
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open conversations store: %w", err)
	}
	defer f.Close()

	var recent []core.Message
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var turn conversationTurn
		if err := json.Unmarshal(scanner.Bytes(), &turn); err != nil {
			log.Debug("skipping malformed conversation turn", "error", err)
			continue
		}
		if turn.Channel != channel {
			continue
		}
		recent = append(recent, core.Message{Role: turn.Role, Content: turn.Content})
		if len(recent) > n {
			recent = recent[len(recent)-n:]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read conversations store: %w", err)
	}
	return recent, nil
}

// LastActivity returns the timestamp of the most recent turn on channel, or
// the zero time if the channel has no turns. Single pass, no message-slice
// allocation; this is what lets episodic consolidation measure the
// session-boundary gap across process restarts.
func (s *ConversationStore) LastActivity(channel string) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("open conversations store: %w", err)
	}
	defer f.Close()

	var last time.Time
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var turn conversationTurn
		if err := json.Unmarshal(scanner.Bytes(), &turn); err != nil {
			continue
		}
		if turn.Channel != channel {
			continue
		}
		if turn.Timestamp.After(last) {
			last = turn.Timestamp
		}
	}
	if err := scanner.Err(); err != nil {
		return time.Time{}, fmt.Errorf("read conversations store: %w", err)
	}
	return last, nil
}

func (s *ConversationStore) Close() error { return nil }

// Search returns turns matching a substring or time predicate. query is
// case-insensitive substring match against the content. since filters to
// turns at or after the timestamp; until filters to turns strictly before
// the timestamp (both zero = no filter). limit caps the returned slice; the
// most recent matches win when more results exist.
//
// This is what powers /recall and "what did I say yesterday" — it scans the
// entire JSONL, not just the sliding LoadRecent window. The upper-bound
// `until` is what makes "yesterday" actually mean yesterday and not
// yesterday-or-later (the previous client-side `trimAfter` was a no-op
// because core.Message has no timestamp).
func (s *ConversationStore) Search(channel, query string, since, until time.Time, limit int) ([]core.Message, error) {
	if limit <= 0 {
		limit = 50
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open conversations store: %w", err)
	}
	defer f.Close()

	q := strings.ToLower(strings.TrimSpace(query))
	hasQuery := q != ""
	hasSince := !since.IsZero()
	hasUntil := !until.IsZero()

	var matches []core.Message
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var turn conversationTurn
		if err := json.Unmarshal(scanner.Bytes(), &turn); err != nil {
			continue
		}
		if channel != "" && turn.Channel != channel {
			continue
		}
		if hasSince && turn.Timestamp.Before(since) {
			continue
		}
		if hasUntil && !turn.Timestamp.Before(until) {
			continue
		}
		if hasQuery && !strings.Contains(strings.ToLower(turn.Content), q) {
			continue
		}
		matches = append(matches, core.Message{Role: turn.Role, Content: turn.Content})
		if len(matches) > limit {
			matches = matches[len(matches)-limit:]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read conversations store: %w", err)
	}
	return matches, nil
}

// rotateLocked keeps the last maxKeepTurns entries and archives the rest.
// Must be called with s.mu held.
func (s *ConversationStore) rotateLocked() {
	f, err := os.Open(s.path)
	if err != nil {
		return
	}
	defer f.Close()

	var lines [][]byte
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		lines = append(lines, append([]byte(nil), scanner.Bytes()...))
	}
	if scanner.Err() != nil || len(lines) <= s.maxKeepTurns {
		return
	}

	// Archive old entries
	_ = os.Rename(s.path, s.path+".old")

	// Write back only the recent entries
	out, err := os.OpenFile(s.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer out.Close()
	keep := lines[len(lines)-s.maxKeepTurns:]
	for _, line := range keep {
		_, _ = out.Write(append(line, '\n'))
	}
	log.Info("conversation store rotated", "kept", len(keep), "archived", len(lines)-len(keep))
}

func (s *ConversationStore) countTurns() (int, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}
