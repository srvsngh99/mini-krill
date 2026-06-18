package chat

import (
	"context"
	"strings"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
	"github.com/srvsngh99/mini-krill/internal/reef"
)

// WebBot is the Reef hub ChatBot: it long-polls the Reef outbox for the owner's
// messages, runs each through the shared core.ChatHandler, and posts replies
// back to the Reef chat channel. It is the headless, web-app equivalent of the
// Telegram and Discord bots and satisfies core.ChatBot.
type WebBot struct {
	handler core.ChatHandler
}

// NewWebBot creates a WebBot bound to the shared chat handler.
func NewWebBot(handler core.ChatHandler) *WebBot {
	return &WebBot{handler: handler}
}

// Platform identifies this bot for owner-gating and notifier routing.
func (w *WebBot) Platform() string { return "web" }

// Start posts a presence ping then long-polls the Reef outbox until ctx is
// cancelled, dispatching each owner message to the handler.
func (w *WebBot) Start(ctx context.Context) error {
	if err := reef.PostIngest("chat", "status", "Mini-Krill online on Reef."); err != nil {
		log.Warn("reef startup ping failed", "error", err)
	}
	log.Info("web (reef) bot started", "agent", reef.AgentID())
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		items, err := reef.PollOutbox(25)
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}
		for _, it := range items {
			text := strings.TrimSpace(it.Text)
			if text == "" {
				continue
			}
			msg := core.ChatMessage{
				Platform: "web", ChatID: "reef", UserID: "owner",
				Username: "owner", Text: text,
			}
			resp, herr := w.handler.HandleMessage(ctx, msg)
			if herr != nil {
				log.Error("web handler error", "error", herr)
				resp = "I hit an error processing that."
			}
			if strings.TrimSpace(resp) == "" {
				continue
			}
			if err := reef.PostIngest("chat", "chat", resp); err != nil {
				log.Error("reef reply post failed", "error", err)
			}
		}
	}
}

// Stop is a no-op; the bot stops when its Start context is cancelled.
func (w *WebBot) Stop() error { return nil }
