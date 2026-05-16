package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// userState models the user as a stateful agent rather than a stateless
// prompt source (#37): what they're working on, what they last asked, the
// mood they last expressed, and when they were last seen. It is injected
// read-only into chat context so replies land in the right register — e.g.
// not opening a 7-step plan when a "you there?" arrives at 1am.
//
// Like episodic memory and long-term memory, this is point-in-time, not live
// truth: it is stamped with LastSeen and surfaced with that caveat so a stale
// focus area can't masquerade as current reality.
type userState struct {
	FocusArea   string    `json:"focus_area"`   // coarse area of work, from the task cluster's object class
	LastCluster string    `json:"last_cluster"` // verb/object of the last task-shaped request
	LastMood    string    `json:"last_mood"`    // positive | negative | neutral, from light sentiment on the last message
	LastSeen    time.Time `json:"last_seen"`
}

// userStateKey is a normal (non-reserved) memory key; sanitizeKey maps ':' to
// '_' on disk, and Recall round-trips through the same sanitiser.
const userStateKey = "userstate:current"

func loadUserState(ctx context.Context, mem core.Memory) userState {
	var us userState
	if mem == nil {
		return us
	}
	e, err := mem.Recall(ctx, userStateKey)
	if err != nil || e == nil || e.Value == "" {
		return us
	}
	if err := json.Unmarshal([]byte(e.Value), &us); err != nil {
		log.Debug("userstate: corrupt record, resetting", "error", err)
		return userState{}
	}
	return us
}

func saveUserState(ctx context.Context, mem core.Memory, us userState) {
	if mem == nil {
		return
	}
	data, err := json.Marshal(us)
	if err != nil {
		return
	}
	now := time.Now()
	if err := mem.Store(ctx, core.MemoryEntry{
		Key:        userStateKey,
		Value:      string(data),
		Tags:       []string{"userstate", "system"},
		Scope:      "system",
		Source:     "reflection",
		CreatedAt:  now,
		AccessedAt: now,
	}); err != nil {
		log.Debug("userstate: store failed", "error", err)
	}
}

// lightMood is a deliberately cheap sentiment read — no LLM call on every
// turn. Mirrors the positive/negative keyword sets recordFeedback uses; any
// ambiguity falls through to neutral.
func lightMood(input string) string {
	lower := strings.ToLower(input)
	for _, p := range []string{"thanks", "thank you", "great", "perfect", "awesome", "love it", "nice", "good job", "brilliant"} {
		if containsWord(lower, p) {
			return "positive"
		}
	}
	for _, n := range []string{"wrong", "terrible", "useless", "bad", "broken", "annoying", "frustrated", "stupid"} {
		if containsWord(lower, n) {
			return "negative"
		}
	}
	return "neutral"
}

// updateUserState refreshes and persists the record from the current message.
// Called once per turn; runs synchronously off the hot path only for the
// in-memory update — persistence is the caller's choice (we go via a
// goroutine in ChatFromPlatform so the file write never blocks the reply).
func updateUserState(ctx context.Context, mem core.Memory, input string) {
	// Load-modify-save across two independently-locked FileMemory ops from an
	// async goroutine: two near-simultaneous turns can lose an update. This is
	// a deliberate, accepted trade-off — the record is read-only context, not
	// behavioural state, so a momentarily stale focus/mood is harmless and not
	// worth a cross-store transaction.
	us := loadUserState(ctx, mem)
	c := ClusterFor(input)
	if c.ID != "" {
		us.LastCluster = c.String()
		if c.ObjectClass != "" {
			us.FocusArea = c.ObjectClass
		}
	}
	us.LastMood = lightMood(input)
	us.LastSeen = time.Now()
	saveUserState(ctx, mem, us)
}

// contextLine renders the state as one inject-ready, explicitly point-in-time
// system line, or "" when there's nothing useful yet.
func (us userState) contextLine() string {
	if us.LastSeen.IsZero() {
		return ""
	}
	parts := []string{}
	if us.FocusArea != "" {
		parts = append(parts, "focus="+us.FocusArea)
	}
	if us.LastCluster != "" {
		parts = append(parts, "last task="+us.LastCluster)
	}
	if us.LastMood != "" {
		parts = append(parts, "mood="+us.LastMood)
	}
	parts = append(parts, fmt.Sprintf("last seen ~%s ago", time.Since(us.LastSeen).Round(time.Minute)))
	return "User context (read-only, point-in-time — do not state it back unless relevant): " + strings.Join(parts, ", ") + "."
}
