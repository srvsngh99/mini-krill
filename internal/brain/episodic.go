package brain

import (
	"context"
	"fmt"
	"sort"
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
	// episodeLatestKey is a single rolling pointer the consolidator overwrites
	// with the newest episode. latestEntry reads it by exact key, so "most
	// recent episode" never depends on Search iteration order.
	// FileMemory.Search("episode_", N) returns the *oldest* N matches —
	// os.ReadDir is lexical and episode keys are monotonic episode_<unixMilli>
	// — so a Search+max scan silently picked the newest-of-the-oldest-N and
	// cross-session continuity died (and dedupe broke) past N episodes.
	episodeLatestKey = "episode:latest"
	// maxEpisodeHistory bounds the episode_* namespace. FileMemory accepts a
	// maxMem but never enforces it, so without an explicit prune the namespace
	// grows without limit.
	maxEpisodeHistory = 50
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
	// Overwrite the rolling pointer so latestEntry/dedupe stay correct no
	// matter how many episodes accumulate.
	ptr := entry
	ptr.Key = episodeLatestKey
	if err := e.mem.Store(ctx, ptr); err != nil {
		return nil, fmt.Errorf("episodic: store latest pointer: %w", err)
	}
	e.pruneHistory(ctx)
	log.Info("episodic: session summarised", "channel", channel, "turns", len(turns))
	return &entry, nil
}

// pruneHistory keeps only the newest maxEpisodeHistory episode_* entries so
// the namespace can't grow without limit. The episode:latest pointer is a
// distinct key and is never pruned. Best-effort: a failed prune must not fail
// consolidation.
func (e *Episodic) pruneHistory(ctx context.Context) {
	entries, err := e.mem.Search(ctx, "episode_", 0) // limit 0 = unbounded
	if err != nil {
		log.Debug("episodic: prune skipped", "error", err)
		return
	}
	hist := entries[:0] // in-place filter; episode:latest has no episode_ prefix
	for _, en := range entries {
		if strings.HasPrefix(en.Key, "episode_") && hasTag(en.Tags, episodeTag) {
			hist = append(hist, en)
		}
	}
	if len(hist) <= maxEpisodeHistory {
		return
	}
	sort.Slice(hist, func(i, j int) bool { return hist[i].CreatedAt.After(hist[j].CreatedAt) })
	for _, en := range hist[maxEpisodeHistory:] {
		if err := e.mem.Forget(ctx, en.Key); err != nil {
			log.Debug("episodic: prune entry failed", "key", en.Key, "error", err)
		}
	}
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
	return fmt.Sprintf("Latest episode (read-only, point-in-time from ~%s ago — verify before relying on it): %s", humanizeAge(time.Since(latest.CreatedAt)), latest.Value), nil
}

// latestEntry returns the newest episode via the rolling pointer, or nil.
// Deliberately an O(1) exact-key read, not a Search scan: FileMemory.Search
// returns the *oldest* matches (see episodeLatestKey), so a scan would strand
// continuity and break dedupe once episodes outnumber the scan limit.
func (e *Episodic) latestEntry(ctx context.Context) (*core.MemoryEntry, error) {
	en, err := e.mem.Recall(ctx, episodeLatestKey)
	if err != nil || en == nil {
		return nil, err
	}
	return en, nil
}

// humanizeAge renders a coarse, human age ("40m", "3h", "2d"). Round(time.Hour)
// printed "~0s ago" for any episode under 30 minutes old.
func humanizeAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "moments"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}
