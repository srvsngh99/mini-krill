// Package chat provides platform-agnostic message handling and concrete
// chat-bot integrations (Telegram, Discord). Every bot funnels messages
// through ChatHandlerImpl so routing logic stays in one place.
package chat

import (
	"context"
	"math/rand"
	"strings"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// ChatHandlerImpl implements core.ChatHandler by forwarding messages to the
// agent and wrapping the result with personality-aware fallbacks.
type ChatHandlerImpl struct {
	agent core.Agent
}

// NewHandler creates a ChatHandlerImpl wired to the given agent.
func NewHandler(agent core.Agent) *ChatHandlerImpl {
	return &ChatHandlerImpl{agent: agent}
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

	resp, err := h.agent.Chat(ctx, msg.Text)
	if err != nil {
		log.Error("agent.Chat failed",
			"platform", msg.Platform,
			"user", msg.Username,
			"error", err,
		)
		return "Bubbles! Something went wrong in the deep... (" + err.Error() + ")", nil
	}

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

// randomFact picks a random entry from core.KrillFacts.
func randomFact() string {
	facts := core.KrillFacts
	if len(facts) == 0 {
		return "I'm a krill of few words right now. Try again?"
	}
	return "Did you know? " + facts[rand.Intn(len(facts))]
}
