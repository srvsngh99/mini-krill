package brain

import (
	"fmt"
	"strings"

	log "github.com/srvsngh99/mini-krill/internal/log"
)

// maxTurnPreview is the character limit for each turn when formatting recovery
// context. Keeps the system prompt from exploding when conversations are long.
const maxTurnPreview = 300

// BuildRecoveryContext loads the last maxTurns turns from the conversation
// store for the given channel and formats them as a human-readable thread.
// Returns an empty string if there is no prior history.
func BuildRecoveryContext(store *ConversationStore, channel string, maxTurns int) string {
	if store == nil || maxTurns <= 0 {
		return ""
	}

	msgs, err := store.LoadRecent(channel, maxTurns)
	if err != nil {
		log.Warn("recovery context load failed", "error", err)
		return ""
	}
	if len(msgs) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, m := range msgs {
		label := "User"
		if m.Role == "assistant" {
			label = "Krill"
		}
		sb.WriteString(fmt.Sprintf("%s: %s\n", label, truncateStr(m.Content, maxTurnPreview)))
	}

	log.Debug("recovery context built", "channel", channel, "turns", len(msgs))
	return sb.String()
}

// BuildEnrichedSystemPrompt appends a recovery context section to the base
// system prompt. If recoveryCtx is empty, the base prompt is returned as-is.
func BuildEnrichedSystemPrompt(baseSysPrompt, recoveryCtx string) string {
	if recoveryCtx == "" {
		return baseSysPrompt
	}
	return baseSysPrompt + "\n\n## Recent Conversation (from last session)\nBelow is your recent conversation. Use it for continuity. Do not mention recovery unless asked:\n" + recoveryCtx
}

// truncateStr shortens s to at most maxLen characters, appending "..." if truncated.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
