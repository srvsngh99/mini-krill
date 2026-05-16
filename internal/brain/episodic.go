package brain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// Episodic produces one-paragraph summaries of a finished session ("episodes")
// and surfaces the most recent one on the next greeting, so the agent picks up
// where it left off instead of starting cold. #29 / D6.
//
// An episode is an ordinary MemoryEntry tagged "episode"; it is a point-in-time
// reflection, never live state — the same discipline long-term memory uses, so
// a stale episode can't be mistaken for current reality.
type Episodic struct {
	mem   core.Memory
	store core.ConversationStore
	llm   core.LLMProvider
}

const (
	episodeTag = "episode"
	// minEpisodeTurns: below this a "session" is too thin to be worth a
	// summary (a lone greeting that got no reply, etc).
	minEpisodeTurns = 4
	// episodeDedupeWindow: never produce a second episode within this window.
	// The agent only calls Consolidate after a >30min activity gap, so this is
	// a belt-and-braces guard against a greeting burst minting duplicates.
	episodeDedupeWindow = 25 * time.Minute
)

// NewEpisodic wires the consolidator. Any nil dependency disables it (every
// method becomes a safe no-op), which keeps tests and headless modes simple.
func NewEpisodic(mem core.Memory, store core.ConversationStore, llm core.LLMProvider) *Episodic {
	return &Episodic{mem: mem, store: store, llm: llm}
}

func (e *Episodic) enabled() bool {
	return e != nil && e.mem != nil && e.store != nil && e.llm != nil
}

// Consolidate summarises the recent turns on `channel` into a single episode
// MemoryEntry and stores it. It is a no-op (nil, nil) when disabled, when the
// session is too thin, or when a fresh episode already exists inside the
// dedupe window.
func (e *Episodic) Consolidate(ctx context.Context, channel string) (*core.MemoryEntry, error) {
	if !e.enabled() {
		return nil, nil
	}
	if recent, _ := e.latestEntry(ctx); recent != nil && time.Since(recent.CreatedAt) < episodeDedupeWindow {
		log.Debug("episodic: skipping, fresh episode within dedupe window")
		return nil, nil
	}

	turns, err := e.store.LoadRecent(channel, 80)
	if err != nil {
		return nil, fmt.Errorf("episodic: load turns: %w", err)
	}
	if len(turns) < minEpisodeTurns {
		return nil, nil
	}

	var b strings.Builder
	for _, m := range turns {
		who := "User"
		if m.Role == "assistant" {
			who = "Krill"
		}
		b.WriteString(who)
		b.WriteString(": ")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}

	resp, err := e.llm.Chat(ctx, []core.Message{
		{Role: "system", Content: "Summarise this session in ONE short paragraph (3-4 sentences, plain text, no preamble or bullet points). State: what the user wanted, what was actually done, and what is still unresolved or pending. Be concrete; name specifics over generalities."},
		{Role: "user", Content: b.String()},
	}, core.WithTemperature(0.2), core.WithMaxTokens(280))
	if err != nil {
		return nil, fmt.Errorf("episodic: summarise: %w", err)
	}
	summary := strings.TrimSpace(resp.Content)
	if summary == "" {
		return nil, nil
	}

	now := time.Now()
	entry := core.MemoryEntry{
		Key:        fmt.Sprintf("episode_%d", now.UnixMilli()),
		Value:      summary,
		Tags:       []string{episodeTag, "system"},
		Scope:      "system",
		Source:     "reflection",
		CreatedAt:  now,
		AccessedAt: now,
	}
	if err := e.mem.Store(ctx, entry); err != nil {
		return nil, fmt.Errorf("episodic: store: %w", err)
	}
	log.Info("episodic: session summarised", "channel", channel, "turns", len(turns))
	return &entry, nil
}

// Latest returns the most recent episode younger than maxAge as a ready-to-
// inject, clearly point-in-time context line, or "" when none qualifies.
func (e *Episodic) Latest(ctx context.Context, maxAge time.Duration) (string, error) {
	if !e.enabled() {
		return "", nil
	}
	latest, err := e.latestEntry(ctx)
	if err != nil || latest == nil {
		return "", err
	}
	if maxAge > 0 && time.Since(latest.CreatedAt) > maxAge {
		return "", nil
	}
	age := time.Since(latest.CreatedAt).Round(time.Hour)
	return fmt.Sprintf("Latest episode (read-only, point-in-time from ~%s ago — verify before relying on it): %s", age, latest.Value), nil
}

// latestEntry returns the newest episode-tagged MemoryEntry, or nil.
func (e *Episodic) latestEntry(ctx context.Context) (*core.MemoryEntry, error) {
	entries, err := e.mem.Search(ctx, "episode_", 50)
	if err != nil {
		return nil, err
	}
	var best *core.MemoryEntry
	for i := range entries {
		if !hasTag(entries[i].Tags, episodeTag) {
			continue
		}
		if best == nil || entries[i].CreatedAt.After(best.CreatedAt) {
			best = &entries[i]
		}
	}
	return best, nil
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}
