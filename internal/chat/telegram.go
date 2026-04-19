package chat

import (
	"context"
	"fmt"
	"math/rand"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// telegramMaxLen is Telegram's hard limit for a single message.
const telegramMaxLen = 4096

// TelegramBot implements core.ChatBot for Telegram.
type TelegramBot struct {
	bot     *tgbotapi.BotAPI
	handler core.ChatHandler
	cfg     config.TelegramConfig
	stopCh  chan struct{}
	done    chan struct{}
}

// NewTelegramBot creates a TelegramBot using the provided config and handler.
// Returns an error if the token is missing or invalid.
func NewTelegramBot(cfg config.TelegramConfig, handler core.ChatHandler) (*TelegramBot, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("telegram token is empty - set telegram.token in config or KRILL_TELEGRAM_TOKEN env var")
	}

	bot, err := tgbotapi.NewBotAPI(cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}

	log.Info("telegram bot authenticated", "username", bot.Self.UserName)

	return &TelegramBot{
		bot:     bot,
		handler: handler,
		cfg:     cfg,
		stopCh:  make(chan struct{}),
		done:    make(chan struct{}),
	}, nil
}

// Platform returns "telegram".
func (t *TelegramBot) Platform() string { return "telegram" }

// Start begins polling for Telegram updates and processing messages.
// It blocks until ctx is cancelled or Stop is called.
func (t *TelegramBot) Start(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := t.bot.GetUpdatesChan(u)

	log.Info("telegram bot started, listening for updates",
		"allowed_ids_count", len(t.cfg.AllowedIDs),
	)

	go func() {
		defer close(t.done)

		for {
			select {
			case <-ctx.Done():
				log.Info("telegram bot stopping (context cancelled)")
				t.bot.StopReceivingUpdates()
				return
			case <-t.stopCh:
				log.Info("telegram bot stopping (stop signal)")
				t.bot.StopReceivingUpdates()
				return
			case update, ok := <-updates:
				if !ok {
					log.Info("telegram updates channel closed")
					return
				}
				if update.Message == nil {
					continue
				}
				t.processUpdate(ctx, update)
			}
		}
	}()

	// Block until the goroutine exits.
	<-t.done
	return nil
}

// Stop signals the bot to shut down gracefully and waits for completion.
func (t *TelegramBot) Stop() error {
	select {
	case <-t.stopCh:
		// Already stopped.
	default:
		close(t.stopCh)
	}
	<-t.done
	log.Info("telegram bot stopped")
	return nil
}

// processUpdate handles a single Telegram update. Panics are recovered so one
// bad message never takes down the whole bot.
func (t *TelegramBot) processUpdate(ctx context.Context, update tgbotapi.Update) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("recovered panic in telegram update handler", "panic", fmt.Sprintf("%v", r))
		}
	}()

	msg := update.Message
	chatID := msg.Chat.ID

	// Access control: if AllowedIDs is non-empty, only listed users may interact.
	if len(t.cfg.AllowedIDs) > 0 && !t.isAllowed(msg.From.ID) {
		log.Warn("telegram message from unauthorised user",
			"user_id", msg.From.ID,
			"username", msg.From.UserName,
		)
		return
	}

	// Handle commands.
	if msg.IsCommand() {
		t.handleCommand(chatID, msg)
		return
	}

	// Acknowledge with a reaction emoji so user knows we received it
	t.react(chatID, msg.MessageID, "eyes")

	// Send typing indicator while processing
	typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	if _, err := t.bot.Send(typing); err != nil {
		log.Debug("failed to send typing action", "error", err)
	}

	chatMsg := core.ChatMessage{
		Platform: "telegram",
		ChatID:   fmt.Sprintf("%d", chatID),
		UserID:   fmt.Sprintf("%d", msg.From.ID),
		Username: msg.From.UserName,
		Text:     msg.Text,
	}

	resp, err := t.handler.HandleMessage(ctx, chatMsg)
	if err != nil {
		log.Error("handler error", "error", err)
		resp = "Bubbles! My handler hit a reef. Try again in a moment."
	}
	if strings.TrimSpace(resp) == "" {
		resp = "..."
	}

	// Swap reaction from eyes to checkmark on success, or warning on error
	if err != nil {
		t.react(chatID, msg.MessageID, "thumbs_down")
	} else {
		t.react(chatID, msg.MessageID, "thumbs_up")
	}

	t.sendLong(chatID, resp)
}

// handleCommand dispatches Telegram bot commands.
func (t *TelegramBot) handleCommand(chatID int64, msg *tgbotapi.Message) {
	switch msg.Command() {
	case "start":
		t.sendMessage(chatID,
			"Hey there! I'm Mini Krill, your crustaceous AI buddy. "+
				"I've been swimming these digital oceans for 130 million CPU cycles. "+
				"What can I help you with?")

	case "help":
		t.sendMessage(chatID, strings.Join([]string{
			"Mini Krill - Commands:",
			"",
			"/start  - Wake me up from the deep",
			"/help   - Show this help text",
			"/status - Check my vital signs",
			"/fact   - Learn something about real krill",
			"/plan   - Start a dive plan for a task",
			"",
			"Or just send me a message and I'll do my best!",
		}, "\n"))

	case "status":
		t.sendMessage(chatID, fmt.Sprintf(
			"Swimming along just fine! Mini Krill %s reporting for duty. "+
				"All systems nominal - bioluminescence at full glow.",
			core.Version,
		))

	case "fact":
		facts := core.KrillFacts
		if len(facts) > 0 {
			t.sendMessage(chatID, "Did you know? "+facts[rand.Intn(len(facts))])
		} else {
			t.sendMessage(chatID, "I seem to have forgotten all my krill facts. That's alarming.")
		}

	case "plan":
		t.sendMessage(chatID,
			"Tell me what you need done and I'll draft a dive plan for your approval! "+
				"Just describe the task in your next message.")

	default:
		t.sendMessage(chatID, fmt.Sprintf(
			"Unknown command /%s. Try /help to see what I can do!",
			msg.Command(),
		))
	}
}

// isAllowed checks whether a user ID is in the AllowedIDs list.
func (t *TelegramBot) isAllowed(userID int64) bool {
	for _, id := range t.cfg.AllowedIDs {
		if id == userID {
			return true
		}
	}
	return false
}

// react sets an emoji reaction on a message via the Telegram Bot API.
// Uses the setMessageReaction endpoint (Bot API 7.2+).
func (t *TelegramBot) react(chatID int64, messageID int, emoji string) {
	params := tgbotapi.Params{}
	params.AddNonZero64("chat_id", chatID)
	params.AddNonZero("message_id", messageID)
	// Build the reaction JSON inline - the library doesn't have native support yet
	reactionJSON := fmt.Sprintf(`[{"type":"emoji","emoji":"%s"}]`, emojiChar(emoji))
	params["reaction"] = reactionJSON
	if _, err := t.bot.MakeRequest("setMessageReaction", params); err != nil {
		log.Debug("failed to set reaction", "emoji", emoji, "error", err)
	}
}

// emojiChar maps short names to actual unicode emoji for reactions.
func emojiChar(name string) string {
	switch name {
	case "eyes":
		return "\U0001F440" // eyes
	case "thumbs_up":
		return "\U0001F44D" // thumbs up
	case "thumbs_down":
		return "\U0001F44E" // thumbs down
	case "fire":
		return "\U0001F525" // fire
	case "check":
		return "\u2705" // green check
	case "thinking":
		return "\U0001F914" // thinking face
	default:
		return "\U0001F44D" // default to thumbs up
	}
}

// sendMessage sends a single text message to the given chat.
func (t *TelegramBot) sendMessage(chatID int64, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	reply := tgbotapi.NewMessage(chatID, text)
	if _, err := t.bot.Send(reply); err != nil {
		log.Error("failed to send telegram message", "chat_id", chatID, "error", err)
	}
}

// sendLong sends a response that may exceed Telegram's 4096-char limit by
// splitting it into multiple messages.
func (t *TelegramBot) sendLong(chatID int64, text string) {
	chunks := chunkMessage(text, telegramMaxLen)
	for _, chunk := range chunks {
		t.sendMessage(chatID, chunk)
	}
}

// chunkMessage splits text into pieces of at most maxLen characters, preferring
// to break at newlines so formatted output stays readable.
func chunkMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			chunks = append(chunks, text)
			break
		}

		// Try to find a newline near the boundary to split cleanly.
		cutPoint := maxLen
		lastNewline := strings.LastIndex(text[:maxLen], "\n")
		if lastNewline > maxLen/2 {
			cutPoint = lastNewline + 1 // include the newline in the current chunk
		}

		chunks = append(chunks, text[:cutPoint])
		text = text[cutPoint:]
	}

	return chunks
}
