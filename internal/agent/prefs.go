// Package agent — typed user-preference layer on top of brain.Memory().
// Free-text memory was the only learning channel before; this gives the
// agent a small set of stable boolean keys it can read and write at decision
// time (e.g. shouldRequireApproval reading pref:no_plan_approval).
package agent

import (
	"context"
	"strings"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// Stable preference keys. Add new ones here so callers can't typo them.
const (
	PrefNoPlanApproval = "pref:no_plan_approval"
	PrefStyleTerse     = "pref:style_terse"
	PrefNoMetaphors    = "pref:no_metaphors"
)

// GetBoolPref returns the boolean value of a typed preference, or `def` if
// the entry is missing or the memory layer is unavailable.
func GetBoolPref(ctx context.Context, mem core.Memory, key string, def bool) bool {
	if mem == nil {
		return def
	}
	entry, err := mem.Recall(ctx, key)
	if err != nil || entry == nil {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(entry.Value)) {
	case "true", "yes", "1", "on":
		return true
	case "false", "no", "0", "off":
		return false
	}
	return def
}

// SetBoolPref writes a typed boolean preference. Stored in user scope so it
// survives across platforms.
func SetBoolPref(ctx context.Context, mem core.Memory, key string, value bool) {
	if mem == nil {
		return
	}
	v := "false"
	if value {
		v = "true"
	}
	entry := core.MemoryEntry{
		Key:        key,
		Value:      v,
		Tags:       []string{"user-preference", "typed", "auto-learned"},
		Scope:      "user",
		Source:     "auto-learned",
		CreatedAt:  time.Now(),
		AccessedAt: time.Now(),
	}
	if err := mem.Store(ctx, entry); err != nil {
		log.Debug("set bool pref failed", "key", key, "error", err)
	}
}

// detectTypedPreference scans the user's input for phrases that should flip
// a typed preference. Returns (key, value, matched). matched=false means the
// caller should fall through to the free-text preference path.
//
// We deliberately keep this list small. Each entry is a phrase the user is
// likely to actually type when they want a behavior change.
func detectTypedPreference(input string) (key string, value bool, matched bool) {
	lower := strings.ToLower(strings.TrimSpace(input))
	if lower == "" {
		return "", false, false
	}

	// "Stop asking me to approve" / "no need for approval" → set true.
	skipApprovalPhrases := []string{
		"no need for approval",
		"no need to approve",
		"no need for taking my approval",
		"no need to take my approval",
		"don't ask for approval",
		"do not ask for approval",
		"stop asking for approval",
		"stop asking me to approve",
		"skip approval",
		"skip the approval",
		"auto approve",
		"auto-approve",
		"just do it",
		"don't ask, just do",
	}
	for _, p := range skipApprovalPhrases {
		if strings.Contains(lower, p) {
			return PrefNoPlanApproval, true, true
		}
	}

	// Inverse: "ask me before doing things" → set false (re-enable approval).
	requireApprovalPhrases := []string{
		"ask me before",
		"always ask before",
		"require approval",
		"please ask first",
		"check with me before",
	}
	for _, p := range requireApprovalPhrases {
		if strings.Contains(lower, p) {
			return PrefNoPlanApproval, false, true
		}
	}

	// Style: terseness — flip on.
	tersePhrases := []string{
		"be terse", "be more terse", "be brief", "be more brief",
		"shorter responses", "keep it short", "be concise", "be more concise",
		"less verbose",
	}
	for _, p := range tersePhrases {
		if strings.Contains(lower, p) {
			return PrefStyleTerse, true, true
		}
	}

	// Style: terseness — flip off (verbose mode). Symmetric inverse so a
	// user who got auto-flipped to terse can recover with natural language.
	verbosePhrases := []string{
		"be verbose", "be more verbose", "be detailed", "more detail",
		"longer responses", "give me more detail", "more thorough",
	}
	for _, p := range verbosePhrases {
		if strings.Contains(lower, p) {
			return PrefStyleTerse, false, true
		}
	}

	// Style: drop the krill metaphors.
	noMetaphorPhrases := []string{
		"no metaphor", "stop with the metaphors", "drop the metaphors",
		"less ocean", "less krill metaphor", "no krill metaphor",
		"stop the ocean talk",
	}
	for _, p := range noMetaphorPhrases {
		if strings.Contains(lower, p) {
			return PrefNoMetaphors, true, true
		}
	}

	return "", false, false
}

// buildStyleDirective composes a short style instruction from the user's
// typed preferences, suitable for injection as a system message. Returns ""
// when no style preferences are set.
//
// This is the "evolves with usage" hook: as the user accrues preferences,
// the persona delta grows; the static soul.go prompt is the floor, this is
// the user-specific overlay.
func buildStyleDirective(ctx context.Context, mem core.Memory) string {
	if mem == nil {
		return ""
	}
	var parts []string
	if GetBoolPref(ctx, mem, PrefStyleTerse, false) {
		parts = append(parts, "Keep responses short and to the point. No filler, no lengthy preambles, no trailing summaries.")
	}
	if GetBoolPref(ctx, mem, PrefNoMetaphors, false) {
		parts = append(parts, "Skip ocean/krill metaphors and ocean-themed framing. Plain technical language only.")
	}
	if GetBoolPref(ctx, mem, PrefNoPlanApproval, false) {
		parts = append(parts, "The user has opted out of plan approval. Don't ask for approval on non-destructive work — just do it.")
	}
	if len(parts) == 0 {
		return ""
	}
	return "User style preferences (apply silently):\n- " + strings.Join(parts, "\n- ")
}
