// Package chat provides platform-agnostic message handling and concrete
// chat-bot integrations (Telegram, Discord). Every bot funnels messages
// through ChatHandlerImpl so routing logic stays in one place.
package chat

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
	"github.com/srvsngh99/mini-krill/internal/reminder"
)

// ChatHandlerImpl implements core.ChatHandler by forwarding messages to the
// agent and wrapping the result with personality-aware fallbacks.
type ChatHandlerImpl struct {
	agent     core.Agent
	reminders *reminder.Store
}

// NewHandler creates a ChatHandlerImpl wired to the given agent.
func NewHandler(agent core.Agent, reminderStore *reminder.Store) *ChatHandlerImpl {
	return &ChatHandlerImpl{agent: agent, reminders: reminderStore}
}

// HandleMessage processes an incoming chat message and returns the agent's
// response. It never returns an empty string - if something goes wrong the
// user still gets a friendly krill-themed reply.
func (h *ChatHandlerImpl) HandleMessage(ctx context.Context, msg core.ChatMessage) (string, error) {
	// Truncate for logging so we don't dump novels into the log stream.
	preview := msg.Text
	if len(preview) > 80 {
		preview = preview[:80] + "..."
	}
	log.Info("incoming message",
		"platform", msg.Platform,
		"user", msg.Username,
		"chat_id", msg.ChatID,
		"text", preview,
	)

	if response, handled := h.handleReminder(msg.Text); handled {
		return response, nil
	}

	resp, err := h.agent.Chat(ctx, msg.Text)
	if err != nil {
		log.Error("agent.Chat failed",
			"platform", msg.Platform,
			"user", msg.Username,
			"error", err,
		)
		return "Bubbles! Something went wrong in the deep... (" + err.Error() + ")", nil
	}

	// Strip internal reasoning prefixes that some LLMs emit.
	resp = cleanInternalPrefixes(resp)

	// Never send an empty message - surface a krill fact instead.
	if strings.TrimSpace(resp) == "" {
		log.Warn("agent returned empty response, sending krill fact",
			"platform", msg.Platform,
			"user", msg.Username,
		)
		return randomFact(), nil
	}

	return resp, nil
}

func (h *ChatHandlerImpl) handleReminder(input string) (string, bool) {
	if h.reminders == nil {
		return "", false
	}
	text := strings.TrimSpace(input)
	lower := strings.ToLower(text)
	for _, prefix := range []string{"remind me to ", "remind me ", "remind "} {
		if strings.HasPrefix(lower, prefix) {
			text = strings.TrimSpace(text[len(prefix):])
			task, due, err := reminder.Parse(text, "", time.Now())
			if err != nil {
				return "I could not parse the reminder time. Try `remind me to check the build in 10 minutes` or `minikrill remind \"check the build\" --at 30m`.", true
			}
			r, err := h.reminders.Add(task, due)
			if err != nil {
				return fmt.Sprintf("Reminder failed: %s", err), true
			}
			return fmt.Sprintf("Reminder %s set for %s: %s", r.ID, r.DueAt.Local().Format("2006-01-02 15:04"), r.Text), true
		}
	}
	return "", false
}

// cleanInternalPrefixes strips internal reasoning markers that LLMs sometimes
// emit as part of their response (e.g. "Plan: greet the user. Hi there!").
func cleanInternalPrefixes(text string) string {
	prefixes := []string{
		"Plan:",
		"plan:",
		"PLAN:",
		"Thinking:",
		"thinking:",
		"THINKING:",
		"Internal:",
		"internal:",
	}
	trimmed := strings.TrimSpace(text)
	for _, prefix := range prefixes {
		if strings.HasPrefix(trimmed, prefix) {
			// Remove everything up to the first sentence boundary after the prefix
			after := strings.TrimSpace(trimmed[len(prefix):])
			// Look for the real response after the planning sentence
			for _, sep := range []string{". ", ".\n", "! ", "!\n"} {
				if idx := strings.Index(after, sep); idx >= 0 && idx < 200 {
					cleaned := strings.TrimSpace(after[idx+len(sep):])
					if cleaned != "" {
						return cleaned
					}
				}
			}
			// If no sentence boundary, just strip the prefix
			return after
		}
	}
	return text
}

// randomFact picks a random entry from core.KrillFacts.
func randomFact() string {
	facts := core.KrillFacts
	if len(facts) == 0 {
		return "I'm a krill of few words right now. Try again?"
	}
	return "Did you know? " + facts[rand.Intn(len(facts))]
}
