package brain

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// ConversationStore persists conversation turns to SQLite so the krill
// remembers what was said across restarts. Each turn is written immediately
// and keyed by channel (cli, telegram, discord, tui) for isolation.
type ConversationStore struct {
	db *sql.DB
}

// NewConversationStore opens (or creates) a SQLite database at dbPath and
// initialises the turns table. WAL mode is enabled for concurrent safety.
func NewConversationStore(dbPath string) (*ConversationStore, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("open conversations db: %w", err)
	}

	if err := initConversationSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	// Count existing turns for the startup log line.
	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM turns").Scan(&count)
	log.Info("conversation store initialized", "path", dbPath, "turns", count)

	return &ConversationStore{db: db}, nil
}

func initConversationSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS turns (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			channel   TEXT     NOT NULL,
			role      TEXT     NOT NULL,
			content   TEXT     NOT NULL,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_turns_channel_ts ON turns(channel, timestamp);
	`)
	if err != nil {
		return fmt.Errorf("init conversation schema: %w", err)
	}
	return nil
}

// SaveTurn writes a single user or assistant message to durable storage.
func (s *ConversationStore) SaveTurn(channel, role, content string) error {
	_, err := s.db.Exec(
		"INSERT INTO turns (channel, role, content) VALUES (?, ?, ?)",
		channel, role, content,
	)
	if err != nil {
		return fmt.Errorf("save turn: %w", err)
	}
	return nil
}

// LoadRecent returns the last n turns for the given channel, ordered oldest-first
// so they can be injected directly into a message history.
func (s *ConversationStore) LoadRecent(channel string, n int) ([]core.Message, error) {
	rows, err := s.db.Query(`
		SELECT role, content FROM (
			SELECT role, content, id FROM turns
			WHERE channel = ?
			ORDER BY id DESC
			LIMIT ?
		) sub ORDER BY id ASC`,
		channel, n,
	)
	if err != nil {
		return nil, fmt.Errorf("load recent turns: %w", err)
	}
	defer rows.Close()

	var msgs []core.Message
	for rows.Next() {
		var m core.Message
		if err := rows.Scan(&m.Role, &m.Content); err != nil {
			return nil, fmt.Errorf("scan turn: %w", err)
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// Close closes the underlying database connection.
func (s *ConversationStore) Close() error {
	return s.db.Close()
}
