package chat

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// telegramMaxLen is Telegram's hard limit for a single message.
const telegramMaxLen = 4096

// botCooldown prevents infinite bot-to-bot reply loops.
// After replying to a bot, wait this long before replying to the same bot again.
const botCooldown = 30 * time.Second

// TelegramBot implements core.ChatBot for Telegram.
type TelegramBot struct {
	bot          *tgbotapi.BotAPI
	handler      core.ChatHandler
	cfg          config.TelegramConfig
	stopCh       chan struct{}
	done         chan struct{}
	botReplies   map[int64]time.Time // tracks last reply to each bot (by user ID)
	botRepliesMu sync.Mutex
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
		bot:        bot,
		handler:    handler,
		cfg:        cfg,
		stopCh:     make(chan struct{}),
		done:       make(chan struct{}),
		botReplies: make(map[int64]time.Time),
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
	isGroup := msg.Chat.IsGroup() || msg.Chat.IsSuperGroup()
	isFromBot := msg.From != nil && msg.From.IsBot
	botUsername := t.bot.Self.UserName

	// In groups, skip AllowedIDs check - the group itself is access control.
	// In DMs, enforce AllowedIDs if configured.
	if !isGroup && len(t.cfg.AllowedIDs) > 0 && !t.isAllowed(msg.From.ID) {
		log.Warn("telegram DM from unauthorised user",
			"user_id", msg.From.ID,
			"username", msg.From.UserName,
		)
		return
	}

	// Handle commands (work in both DMs and groups).
	if msg.IsCommand() {
		t.handleCommand(chatID, msg)
		return
	}

	// --- Group chat awareness ---
	if isGroup {
		mentioned := t.isMentioned(msg, botUsername)
		isReplyToMe := msg.ReplyToMessage != nil &&
			msg.ReplyToMessage.From != nil &&
			msg.ReplyToMessage.From.UserName == botUsername

		// Bot-to-bot loop prevention: cooldown after replying to same bot
		if isFromBot && !t.canReplyToBot(msg.From.ID) {
			log.Debug("skipping bot message (cooldown active)", "bot", msg.From.UserName)
			return
		}

		// Decide if we should respond based on context
		shouldRespond := mentioned || isReplyToMe || t.isRelevantGroupMessage(msg, botUsername)

		if !shouldRespond {
			log.Debug("group message not relevant, staying quiet",
				"from", msg.From.UserName,
				"text_preview", truncateLog(extractMessageText(msg), 50),
			)
			return
		}

		log.Info("group message relevant, responding",
			"from", msg.From.UserName,
			"mentioned", mentioned,
			"reply_to_me", isReplyToMe,
			"is_bot", isFromBot,
		)
	}

	// Extract text from all message types
	messageText := extractMessageText(msg)
	if strings.TrimSpace(messageText) == "" {
		t.react(chatID, msg.MessageID, "eyes")
		return
	}

	// Strip @mention from message text so the agent gets clean input
	messageText = stripMention(messageText, botUsername)

	// Add context about who's talking (useful in groups)
	if isGroup && msg.From != nil {
		senderName := msg.From.FirstName
		if senderName == "" {
			senderName = msg.From.UserName
		}
		if isFromBot {
			messageText = fmt.Sprintf("[Message from bot @%s in group chat]: %s", msg.From.UserName, messageText)
		} else {
			messageText = fmt.Sprintf("[Message from %s in group chat]: %s", senderName, messageText)
		}
	}

	// Acknowledge
	t.react(chatID, msg.MessageID, "eyes")

	// Typing indicator
	typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	if _, err := t.bot.Send(typing); err != nil {
		log.Debug("failed to send typing action", "error", err)
	}

	chatMsg := core.ChatMessage{
		Platform: "telegram",
		ChatID:   fmt.Sprintf("%d", chatID),
		UserID:   fmt.Sprintf("%d", msg.From.ID),
		Username: msg.From.UserName,
		Text:     messageText,
	}

	resp, err := t.handler.HandleMessage(ctx, chatMsg)
	if err != nil {
		log.Error("handler error", "error", err)
		resp = "Bubbles! My handler hit a reef. Try again in a moment."
	}
	if strings.TrimSpace(resp) == "" {
		resp = "..."
	}

	// Track bot reply for cooldown
	if isFromBot {
		t.recordBotReply(msg.From.ID)
	}

	// Swap reaction
	if err != nil {
		t.react(chatID, msg.MessageID, "thumbs_down")
	} else {
		t.react(chatID, msg.MessageID, "thumbs_up")
	}

	// In groups, reply to the original message for context
	if isGroup {
		t.replyLong(chatID, msg.MessageID, resp)
	} else {
		t.sendLong(chatID, resp)
	}
}

// isRelevantGroupMessage decides if the krill should chime in on a group message
// it wasn't directly mentioned in. Krill are social creatures - they don't just
// wait to be called, they participate when they have something to add.
func (t *TelegramBot) isRelevantGroupMessage(msg *tgbotapi.Message, botUsername string) bool {
	text := strings.ToLower(extractMessageText(msg))
	if text == "" {
		return false
	}

	// Always respond to questions directed at the group
	if strings.Contains(text, "?") && !strings.HasPrefix(text, "[the user sent") {
		return true
	}

	// Respond if someone mentions "krill" by name
	if strings.Contains(text, "krill") || strings.Contains(text, "mini krill") {
		return true
	}

	// Respond to messages from other bots (they might be talking about something relevant)
	if msg.From != nil && msg.From.IsBot {
		return true
	}

	// Respond to opinions, debates, asks for help
	opinionTriggers := []string{
		"what do you think", "any thoughts", "opinions",
		"help me", "can someone", "anyone know",
		"agree", "disagree", "debate",
		"interesting", "cool", "awesome", "amazing",
		"wrong", "right", "correct", "incorrect",
	}
	for _, trigger := range opinionTriggers {
		if strings.Contains(text, trigger) {
			return true
		}
	}

	// Respond to technical/AI topics the krill would have opinions on
	techTriggers := []string{
		"ai", "llm", "model", "agent", "bot",
		"code", "programming", "deploy", "build",
		"search", "data", "memory", "brain",
	}
	matchCount := 0
	for _, trigger := range techTriggers {
		if strings.Contains(text, trigger) {
			matchCount++
		}
	}
	if matchCount >= 2 {
		return true
	}

	return false
}

func truncateLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// isMentioned checks if the bot is @mentioned in the message text or entities.
func (t *TelegramBot) isMentioned(msg *tgbotapi.Message, botUsername string) bool {
	// Check text for @mention
	if strings.Contains(strings.ToLower(msg.Text), "@"+strings.ToLower(botUsername)) {
		return true
	}
	if strings.Contains(strings.ToLower(msg.Caption), "@"+strings.ToLower(botUsername)) {
		return true
	}
	// Check entities for mention type
	for _, e := range msg.Entities {
		if e.Type == "mention" {
			mention := msg.Text[e.Offset : e.Offset+e.Length]
			if strings.EqualFold(mention, "@"+botUsername) {
				return true
			}
		}
	}
	return false
}

// stripMention removes @botusername from message text.
func stripMention(text, botUsername string) string {
	text = strings.ReplaceAll(text, "@"+botUsername, "")
	text = strings.ReplaceAll(text, "@"+strings.ToLower(botUsername), "")
	return strings.TrimSpace(text)
}

// canReplyToBot checks if we can reply to a bot (cooldown not active).
func (t *TelegramBot) canReplyToBot(botID int64) bool {
	t.botRepliesMu.Lock()
	defer t.botRepliesMu.Unlock()
	last, ok := t.botReplies[botID]
	if !ok {
		return true
	}
	return time.Since(last) > botCooldown
}

// recordBotReply records that we replied to a bot (starts cooldown).
func (t *TelegramBot) recordBotReply(botID int64) {
	t.botRepliesMu.Lock()
	defer t.botRepliesMu.Unlock()
	t.botReplies[botID] = time.Now()
}

// replyLong sends a reply to a specific message, splitting if needed.
func (t *TelegramBot) replyLong(chatID int64, replyTo int, text string) {
	chunks := chunkMessage(text, telegramMaxLen)
	for i, chunk := range chunks {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		reply := tgbotapi.NewMessage(chatID, chunk)
		reply.ParseMode = "Markdown"
		if i == 0 {
			reply.ReplyToMessageID = replyTo // only first chunk replies
		}
		if _, err := t.bot.Send(reply); err != nil {
			// Retry without markdown
			reply.ParseMode = ""
			if _, err := t.bot.Send(reply); err != nil {
				log.Error("failed to send reply", "chat_id", chatID, "error", err)
			}
		}
	}
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

// extractMessageText builds a text representation from any Telegram message type.
// Krill have compound eyes that see everything - this function sees every message type.
func extractMessageText(msg *tgbotapi.Message) string {
	// Regular text message (may contain emojis inline)
	if msg.Text != "" {
		return msg.Text
	}

	// Caption on media (photos, videos, GIFs with captions)
	if msg.Caption != "" {
		return msg.Caption
	}

	// Sticker - translate to descriptive text the LLM can understand
	if msg.Sticker != nil {
		emoji := msg.Sticker.Emoji
		setName := msg.Sticker.SetName
		if emoji != "" && setName != "" {
			return fmt.Sprintf("[The user sent a sticker: %s from pack '%s'. Respond to the emotion/meaning behind this sticker naturally.]", emoji, setName)
		}
		if emoji != "" {
			return fmt.Sprintf("[The user sent a sticker with emoji: %s. Respond to the emotion/meaning behind it naturally.]", emoji)
		}
		return "[The user sent a sticker. React playfully.]"
	}

	// GIF / Animation
	if msg.Animation != nil {
		name := msg.Animation.FileName
		if name != "" {
			return fmt.Sprintf("[The user sent a GIF: '%s'. React to it playfully and naturally, as if you can see it.]", name)
		}
		return "[The user sent a GIF/animation. React to it playfully - match their energy.]"
	}

	// Photo
	if msg.Photo != nil && len(msg.Photo) > 0 {
		return "[The user sent a photo. Acknowledge it warmly - you can't see images yet but respond naturally.]"
	}

	// Video
	if msg.Video != nil {
		return "[The user sent a video. Acknowledge it - you can't watch videos yet but respond naturally.]"
	}

	// Voice message
	if msg.Voice != nil {
		return "[The user sent a voice message. You can't hear audio yet - let them know warmly.]"
	}

	// Document/file
	if msg.Document != nil {
		name := msg.Document.FileName
		if name != "" {
			return fmt.Sprintf("[The user sent a file: '%s'. Acknowledge it naturally.]", name)
		}
		return "[The user sent a document. Acknowledge it naturally.]"
	}

	// Contact
	if msg.Contact != nil {
		return fmt.Sprintf("[The user shared a contact: %s %s. Acknowledge it.]",
			msg.Contact.FirstName, msg.Contact.LastName)
	}

	// Location
	if msg.Location != nil {
		return fmt.Sprintf("[The user shared a location: lat %.4f, lon %.4f. Acknowledge it.]",
			msg.Location.Latitude, msg.Location.Longitude)
	}

	// Dice/game
	if msg.Dice != nil {
		return fmt.Sprintf("[The user rolled a %s and got %d. React to the result!]",
			msg.Dice.Emoji, msg.Dice.Value)
	}

	return ""
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
// Uses Markdown parse mode so LLM formatting renders natively in Telegram.
func (t *TelegramBot) sendMessage(chatID int64, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	reply := tgbotapi.NewMessage(chatID, text)
	reply.ParseMode = "Markdown"
	if _, err := t.bot.Send(reply); err != nil {
		// Markdown parsing can fail on malformed output - retry as plain text
		log.Debug("markdown send failed, retrying as plain text", "error", err)
		reply.ParseMode = ""
		if _, err := t.bot.Send(reply); err != nil {
			log.Error("failed to send telegram message", "chat_id", chatID, "error", err)
		}
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
