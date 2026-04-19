// Package brain implements Mini Krill's cognitive core: memory, soul, personality,
// and heartbeat. Like the krill's 130-million-year-old nervous system, it is
// simple, resilient, and surprisingly effective.
package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// sanitizeKey replaces non-alphanumeric characters with underscores.
var sanitizeRe = regexp.MustCompile(`[^a-zA-Z0-9]`)

func sanitizeKey(key string) string {
	return sanitizeRe.ReplaceAllString(key, "_")
}

// FileMemory implements core.Memory using JSON files on disk.
// Each memory entry is stored as an individual JSON file in the memories
// subdirectory, named by its sanitized key. Thread-safe via sync.RWMutex.
type FileMemory struct {
	dir     string
	mu      sync.RWMutex
	maxMem  int
}

// NewFileMemory creates a new FileMemory rooted at the given directory.
// It ensures the directory exists before returning.
func NewFileMemory(dir string, maxMemories int) (*FileMemory, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create memory dir %s: %w", dir, err)
	}
	fm := &FileMemory{
		dir:    dir,
		maxMem: maxMemories,
	}
	log.Info("memory store initialized", "dir", dir, "max", maxMemories)
	return fm, nil
}

// entryPath returns the filesystem path for a given memory key.
func (m *FileMemory) entryPath(key string) string {
	return filepath.Join(m.dir, sanitizeKey(key)+".json")
}

// Store persists a memory entry to disk as a JSON file.
// If an entry with the same key already exists, it is overwritten.
func (m *FileMemory) Store(_ context.Context, entry core.MemoryEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Set timestamps if not already set
	now := time.Now().UTC()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.AccessedAt = now

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal memory entry %q: %w", entry.Key, err)
	}

	path := m.entryPath(entry.Key)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write memory %q: %w", entry.Key, err)
	}

	log.Debug("memory stored", "key", entry.Key, "path", path)
	return nil
}

// Recall retrieves a single memory entry by exact key.
// Returns nil and no error if the key does not exist.
func (m *FileMemory) Recall(_ context.Context, key string) (*core.MemoryEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := m.entryPath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read memory %q: %w", key, err)
	}

	var entry core.MemoryEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("unmarshal memory %q: %w", key, err)
	}

	// Update access time (best-effort)
	entry.AccessedAt = time.Now().UTC()
	if updated, err := json.MarshalIndent(entry, "", "  "); err == nil {
		_ = os.WriteFile(path, updated, 0644)
	}

	return &entry, nil
}

// Search performs a case-insensitive substring match against memory keys and values.
// Results are returned up to the specified limit.
func (m *FileMemory) Search(_ context.Context, query string, limit int) ([]core.MemoryEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, err := m.listAll()
	if err != nil {
		return nil, err
	}

	queryLower := strings.ToLower(query)
	var results []core.MemoryEntry

	for _, entry := range entries {
		if limit > 0 && len(results) >= limit {
			break
		}
		keyLower := strings.ToLower(entry.Key)
		valueLower := strings.ToLower(entry.Value)

		if strings.Contains(keyLower, queryLower) || strings.Contains(valueLower, queryLower) {
			results = append(results, entry)
		}
	}

	log.Debug("memory search complete", "query", query, "found", len(results))
	return results, nil
}

// Forget removes a memory entry by key.
// Returns no error if the key does not exist (idempotent, like krill shedding an exoskeleton).
func (m *FileMemory) Forget(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := m.entryPath(key)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("forget memory %q: %w", key, err)
	}

	log.Debug("memory forgotten", "key", key)
	return nil
}

// List returns all stored memory entries.
func (m *FileMemory) List(_ context.Context) ([]core.MemoryEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.listAll()
}

// Count returns the number of stored memory entries.
// Fast count via directory listing - no need to parse every file.
func (m *FileMemory) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	dirEntries, err := os.ReadDir(m.dir)
	if err != nil {
		log.Error("count memories failed", "error", err)
		return 0
	}

	count := 0
	for _, de := range dirEntries {
		if !de.IsDir() && strings.HasSuffix(de.Name(), ".json") {
			count++
		}
	}
	return count
}

// listAll reads all memory files from disk. Caller must hold at least a read lock.
func (m *FileMemory) listAll() ([]core.MemoryEntry, error) {
	dirEntries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, fmt.Errorf("list memory dir: %w", err)
	}

	var entries []core.MemoryEntry
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(m.dir, de.Name()))
		if err != nil {
			log.Warn("skip unreadable memory file", "file", de.Name(), "error", err)
			continue
		}

		var entry core.MemoryEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			log.Warn("skip malformed memory file", "file", de.Name(), "error", err)
			continue
		}

		entries = append(entries, entry)
	}

	return entries, nil
}
