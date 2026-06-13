package chat

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
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

// defaultBotMaxTurns is how many bot-to-bot exchanges are allowed
// before the krill waits for a human to speak. Configurable via
// telegram.bot_max_turns in config.yaml.
const defaultBotMaxTurns = 3

// LearnFunc is called to store group conversation insights to memory.
type LearnFunc func(ctx context.Context, key, value string) error

// TelegramBot implements core.ChatBot for Telegram.
type TelegramBot struct {
	bot         *tgbotapi.BotAPI
	handler     core.ChatHandler
	cfg         config.TelegramConfig
	stopCh      chan struct{}
	done        chan struct{}
	botTurns    map[int64]int // tracks consecutive bot-to-bot exchanges per chat
	botTurnsMu  sync.Mutex
	learnFn     LearnFunc            // optional: store group learnings to memory
	providerMgr core.ProviderControl // optional: runtime model/provider switching
	ownerWarn   sync.Once            // emits the "owner_id unset" hint at most once
}

// isOwner reports whether id is the configured single owner. When OwnerID is
// unset (0) this is always false; callers treat unset as "owner-gating off".
func (t *TelegramBot) isOwner(id int64) bool {
	return t.cfg.OwnerID != 0 && id == t.cfg.OwnerID
}

// bystanderDecline is the only thing a non-owner who directly addresses the
// bot gets back. It never reaches the agent, so no task/memory/destructive
// side effects are possible from a bystander.
const bystanderDecline = "Hey! 🦐 I only take instructions from my owner, so I can't run tasks or remember things for anyone else here — but say hi to them for me."

// ownerGate is the outcome of the single-owner authorization decision.
type ownerGate int

const (
	gateProceed ownerGate = iota // owner, or owner-gating off (legacy) — route to agent
	gateIgnore                   // bystander not addressing the bot — drop silently
	gateDecline                  // bystander addressing the bot — fixed decline, never routed
)

// decideOwnerGate is the single authoritative owner-authorization decision,
// kept pure (no tgbotapi / I/O) so the boundary is unit-testable: a future
// refactor of processUpdate has a test guarding "owner-gating on + not the
// owner (incl. From==nil) never proceeds to the agent". ownerSet==false means
// gating is off → always proceed (legacy behaviour). Deny-by-default: anything
// that isn't a confirmed owner under an active gate is ignored or declined,
// never proceeded.
func decideOwnerGate(ownerSet, isOwner, addressed bool) ownerGate {
	if !ownerSet || isOwner {
		return gateProceed
	}
	if addressed {
		return gateDecline
	}
	return gateIgnore
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
		bot:      bot,
		handler:  handler,
		cfg:      cfg,
		stopCh:   make(chan struct{}),
		done:     make(chan struct{}),
		botTurns: make(map[int64]int),
	}, nil
}

// SetLearnFunc sets a callback for storing group conversation learnings.
func (t *TelegramBot) SetLearnFunc(fn LearnFunc) {
	t.learnFn = fn
}

// SetProviderManager injects the provider manager for runtime model switching.
func (t *TelegramBot) SetProviderManager(mgr core.ProviderControl) {
	t.providerMgr = mgr
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
				if update.CallbackQuery != nil {
					t.handleCallbackQuery(update.CallbackQuery)
					continue
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

	// --- Owner authorization (single-owner model) ---
	// When owner_id is set, only the owner drives the bot. A bystander in a
	// shared group/chat is replied to ONLY if they directly address the bot,
	// with a fixed decline that never reaches the agent — so a stranger can
	// never trigger tasks, memory writes, or destructive actions. This is the
	// safety counterpart to "no approval prompts by default". When owner_id is
	// unset, behaviour is unchanged (legacy) with a one-time hint.
	ownerSet := t.cfg.OwnerID != 0
	if !ownerSet {
		t.ownerWarn.Do(func() {
			log.Warn("telegram.owner_id is unset — anyone in a shared chat can drive the bot; set telegram.owner_id to your Telegram user ID for single-owner safety")
		})
	}
	// msg.From == nil (linked-channel auto-forwards, sender_chat/anonymous
	// posts) is never the owner — decideOwnerGate then yields ignore/decline
	// (never proceed) when owner-gating is on, so the safety property is the
	// gate's, not an incidental downstream nil-deref + recover.
	isOwner := msg.From != nil && t.isOwner(msg.From.ID)
	addressed := t.isMentioned(msg, botUsername) ||
		(msg.ReplyToMessage != nil &&
			msg.ReplyToMessage.From != nil &&
			msg.ReplyToMessage.From.UserName == botUsername)
	switch decideOwnerGate(ownerSet, isOwner, addressed) {
	case gateProceed:
		// Owner, or owner-gating off (legacy) — fall through to handling.
	case gateDecline:
		var uid int64
		var uname string
		if msg.From != nil {
			uid, uname = msg.From.ID, msg.From.UserName
		}
		log.Info("bystander addressed the bot; declining (owner-gated)",
			"user_id", uid, "username", uname)
		// The decline deliberately bypasses bot-to-bot turn accounting: it's
		// a single fixed reply that never reaches the agent, so it cannot
		// drive a conversational loop.
		t.sendMessage(chatID, bystanderDecline)
		return
	case gateIgnore:
		var uid int64
		var uname string
		if msg.From != nil {
			uid, uname = msg.From.ID, msg.From.UserName
		}
		log.Debug("bystander message ignored (owner-gated)",
			"user_id", uid, "username", uname)
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

		// Human spoke -> reset bot turn counter for this chat.
		// This is the key to preventing loops while allowing conversation:
		// bots can exchange N turns, then wait for human input.
		if !isFromBot {
			t.resetBotTurns(chatID)
		}

		// Bot-to-bot: check turn limit
		maxTurns := t.cfg.BotMaxTurns
		if maxTurns == 0 {
			maxTurns = defaultBotMaxTurns
		}
		if isFromBot && !mentioned && !isReplyToMe {
			turns := t.getBotTurns(chatID)
			if turns >= maxTurns {
				log.Debug("bot turn limit reached, waiting for human",
					"bot", msg.From.UserName,
					"turns", turns,
					"max", maxTurns,
				)
				return
			}
		}

		// Skip very short noise
		text := strings.TrimSpace(extractMessageText(msg))
		if len(text) < 3 {
			return
		}

		// If the message explicitly mentions a different bot, don't respond.
		// This prevents identity confusion where our bot answers for another bot.
		if !mentioned && !isReplyToMe && mentionsOtherBot(text, botUsername) {
			log.Debug("message mentions another bot, skipping",
				"from", msg.From.UserName,
				"text_preview", truncateLog(text, 50),
			)
			return
		}

		log.Info("group message, participating",
			"from", msg.From.UserName,
			"mentioned", mentioned,
			"reply_to_me", isReplyToMe,
			"is_bot", isFromBot,
			"bot_turns", t.getBotTurns(chatID),
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

	// Natural language provider switching: "switch to claude", "use gemini", etc.
	if t.providerMgr != nil {
		if target := detectNLSwitch(messageText); target != "" {
			provider, model, ok := t.providerMgr.ResolveTarget(target)
			if ok {
				if err := t.providerMgr.Switch(provider, model); err != nil {
					t.sendMessage(chatID, fmt.Sprintf("Switch failed: %s", err.Error()))
				} else {
					info := t.providerMgr.ActiveInfo()
					t.sendMessage(chatID, fmt.Sprintf("Switched to %s (%s)", info.Provider, info.Model))
				}
				return
			}
		}
	}

	// Add group context so the agent knows who's talking and behaves naturally.
	// The krill should be a group participant, not just a responder.
	if isGroup && msg.From != nil {
		senderName := msg.From.FirstName
		if senderName == "" {
			senderName = msg.From.UserName
		}

		mentioned := t.isMentioned(msg, botUsername)
		isReplyToMe := msg.ReplyToMessage != nil &&
			msg.ReplyToMessage.From != nil &&
			msg.ReplyToMessage.From.UserName == botUsername

		if mentioned || isReplyToMe {
			// Directly addressed - respond fully
			if isFromBot {
				messageText = fmt.Sprintf("[Group chat - @%s is talking to you directly]: %s", msg.From.UserName, messageText)
			} else {
				messageText = fmt.Sprintf("[Group chat - %s is talking to you directly]: %s", senderName, messageText)
			}
		} else if isFromBot {
			// Another bot said something - respond as a fellow group member
			messageText = fmt.Sprintf("[Group chat - bot @%s said this. You are both in a group with the user. Respond naturally as a group participant. Keep it brief and conversational. Add your perspective if you have one, or react naturally. Don't repeat what the other bot said.]: %s", msg.From.UserName, messageText)
		} else {
			// User said something to the group - participate naturally
			messageText = fmt.Sprintf("[Group chat - %s said this to the group. You and other bots are group members. Respond naturally and briefly like a friend in a group chat. Don't be overly formal or assistant-like. If you have nothing meaningful to add, keep it very short.]: %s", senderName, messageText)
		}
	}

	// Acknowledge
	t.react(chatID, msg.MessageID, "eyes")

	// Typing indicator
	typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	if _, err := t.bot.Send(typing); err != nil {
		log.Debug("failed to send typing action", "error", err)
	}

	// Providers manage their own per-call deadlines, but the turn as a whole
	// gets a generous backstop: on 2026-05-16 a provider hang left a message
	// unanswered for 16+ minutes until a restart silently dropped it. With a
	// ceiling, the worst case is a late apology instead of eternal silence.
	msgCtx, msgCancel := context.WithTimeout(ctx, turnDeadline)
	defer msgCancel()

	chatMsg := core.ChatMessage{
		Platform: "telegram",
		ChatID:   fmt.Sprintf("%d", chatID),
		UserID:   fmt.Sprintf("%d", msg.From.ID),
		Username: msg.From.UserName,
		Text:     messageText,
	}

	// The handler owns all fallback wording (including the turnDeadline
	// apology) and persists it, so durable history matches what the user
	// saw. This block only fires if HandleMessage itself returns a non-nil
	// error — exceptional, since the handler normally swallows failures into
	// a recorded fallback. Persist it here so even that path leaves a trace.
	resp, err := t.handler.HandleMessage(msgCtx, chatMsg)
	handlerFailed := err != nil
	if handlerFailed {
		log.Error("handler error", "chat_id", chatID, "error", err)
		resp = "Bubbles! My handler hit a reef. Try again in a moment."
		if tr, ok := t.handler.(interface{ RecordTurn(role, content string) }); ok {
			tr.RecordTurn("assistant", resp)
		}
	}
	if strings.TrimSpace(resp) == "" {
		resp = "..."
	}

	// Check for cross-channel message directives in the response
	resp, crossTargets := extractCrossPostDirectives(resp)
	// #22: belt-and-braces — strip any malformed/unclosed CROSSPOST literal
	// that the parser left behind so the raw token never reaches the user.
	if scrubbed, did := scrubCrosspostLiterals(resp); did {
		log.Warn("stripped residual CROSSPOST literal from response", "chat_id", chatID)
		resp = scrubbed
	}
	for _, ct := range crossTargets {
		targetID, parseErr := strconv.ParseInt(ct.ChatID, 10, 64)
		if parseErr != nil {
			log.Warn("invalid cross-post target chat ID", "id", ct.ChatID)
			continue
		}
		t.SendToChat(targetID, ct.Text)
		log.Info("cross-posted message", "target_chat", ct.ChatID)
	}
	if strings.TrimSpace(resp) == "" && len(crossTargets) > 0 {
		resp = "Done! Message sent to the other chat."
	}

	// Track bot-to-bot turn
	if isFromBot {
		t.incrementBotTurns(chatID)
	}

	// Swap reaction: eyes → thumbs_up on success, eyes → thumbs_down on failure
	if handlerFailed {
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

	// Learn from interactions - store interesting exchanges
	senderName := ""
	if msg.From != nil {
		senderName = msg.From.UserName
		if senderName == "" {
			senderName = msg.From.FirstName
		}
	}
	if isGroup && t.handler != nil {
		go t.maybeLearnFromGroup(ctx, senderName, extractMessageText(msg), resp)
	} else if !isGroup && t.learnFn != nil {
		go t.maybeLearnFromDM(ctx, senderName, extractMessageText(msg), resp)
	}
}

func truncateLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// maybeLearnFromGroup stores interesting group exchanges to memory so the
// krill gets better over time. Only stores substantial exchanges.
func (t *TelegramBot) maybeLearnFromGroup(ctx context.Context, sender, input, response string) {
	if t.learnFn == nil {
		return
	}
	// Only learn from substantial exchanges (not greetings or one-word messages)
	if len(input) < 20 || len(response) < 20 {
		return
	}
	// Store as a group conversation memory
	key := fmt.Sprintf("group_%s_%d", sender, time.Now().Unix())
	value := fmt.Sprintf("%s said: %s\nKrill responded: %s", sender, truncateLog(input, 200), truncateLog(response, 200))
	if err := t.learnFn(ctx, key, value); err != nil {
		log.Debug("failed to store group learning", "error", err)
	}
}

// maybeLearnFromDM stores DM conversation exchanges to memory so the krill
// can recall previous DM conversations across restarts.
func (t *TelegramBot) maybeLearnFromDM(ctx context.Context, sender, input, response string) {
	if t.learnFn == nil {
		return
	}
	// Lower threshold than group - DMs are direct interactions worth remembering
	if len(input) < 10 || len(response) < 10 {
		return
	}
	key := fmt.Sprintf("dm_%s_%d", sender, time.Now().Unix())
	value := fmt.Sprintf("DM with %s: user said: %s\nKrill responded: %s",
		sender, truncateLog(input, 200), truncateLog(response, 200))
	if err := t.learnFn(ctx, key, value); err != nil {
		log.Debug("failed to store DM learning", "error", err)
	}
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

// mentionsOtherBot returns true if the text contains an @mention that is NOT
// for this bot. This prevents the krill from answering when someone talks to
// a different bot in the group.
func mentionsOtherBot(text, myUsername string) bool {
	lower := strings.ToLower(text)
	myMention := "@" + strings.ToLower(myUsername)
	idx := 0
	for {
		atPos := strings.Index(lower[idx:], "@")
		if atPos == -1 {
			break
		}
		absPos := idx + atPos
		// Extract the username after @
		end := absPos + 1
		for end < len(lower) && (lower[end] >= 'a' && lower[end] <= 'z' ||
			lower[end] >= '0' && lower[end] <= '9' || lower[end] == '_') {
			end++
		}
		mention := lower[absPos:end]
		if len(mention) > 1 && mention != myMention {
			return true // found a mention that's not us
		}
		idx = end
		if idx >= len(lower) {
			break
		}
	}
	return false
}

// getBotTurns returns the current bot-to-bot exchange count for a chat.
func (t *TelegramBot) getBotTurns(chatID int64) int {
	t.botTurnsMu.Lock()
	defer t.botTurnsMu.Unlock()
	return t.botTurns[chatID]
}

// incrementBotTurns adds one to the bot exchange counter for a chat.
func (t *TelegramBot) incrementBotTurns(chatID int64) {
	t.botTurnsMu.Lock()
	defer t.botTurnsMu.Unlock()
	t.botTurns[chatID]++
}

// resetBotTurns resets the bot exchange counter when a human speaks.
func (t *TelegramBot) resetBotTurns(chatID int64) {
	t.botTurnsMu.Lock()
	defer t.botTurnsMu.Unlock()
	t.botTurns[chatID] = 0
}

// replyLong sends a reply to a specific message, splitting if needed.
func (t *TelegramBot) replyLong(chatID int64, replyTo int, text string) {
	text = sanitizeMarkdown(text)
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
			"/start   - Wake me up from the deep",
			"/help    - Show this help text",
			"/status  - Check my vital signs",
			"/model   - Show active provider and model",
			"/models  - List all providers and models",
			"/switch  - Switch provider (e.g. /switch local, /switch codex)",
			"/fact    - Learn something about real krill",
			"/plan    - Start a dive plan for a task",
			"/tasks   - List active and recent tasks",
			"/task ID - Show details of a specific task",
			"/cancel ID - Cancel a running task",
			"",
			"Or say 'remember that ...', 'what do you remember', or 'switch to claude' naturally!",
		}, "\n"))

	case "status":
		t.sendMessage(chatID, fmt.Sprintf(
			"Swimming along just fine! Mini Krill %s reporting for duty. "+
				"All systems nominal - bioluminescence at full glow.",
			core.Version,
		))

	case "fact":
		facts := core.KrillFacts
		t.sendMessage(chatID, "Did you know? "+facts[rand.Intn(len(facts))])

	case "model":
		t.handleModelCommand(chatID, msg)

	case "models":
		t.handleModelsCommand(chatID, msg)

	case "switch":
		t.handleSwitchCommand(chatID, msg)

	case "plan":
		t.sendMessage(chatID,
			"Tell me what you need done and I'll draft a dive plan for your approval! "+
				"Just describe the task in your next message.")

	case "tasks", "task", "cancel":
		// Task management commands are handled by the agent via Chat()
		t.handleTaskViaAgent(chatID, msg)

	default:
		t.sendMessage(chatID, fmt.Sprintf(
			"Unknown command /%s. Try /help to see what I can do!",
			msg.Command(),
		))
	}
}

// handleTaskViaAgent forwards task management commands (/tasks, /task, /cancel)
// through the normal agent Chat() path so the agent handles them.
func (t *TelegramBot) handleTaskViaAgent(chatID int64, msg *tgbotapi.Message) {
	fullText := "/" + msg.Command()
	if args := msg.CommandArguments(); args != "" {
		fullText += " " + args
	}

	msgCtx, msgCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer msgCancel()

	chatMsg := core.ChatMessage{
		Platform: "telegram",
		ChatID:   fmt.Sprintf("%d", chatID),
		UserID:   fmt.Sprintf("%d", msg.From.ID),
		Username: msg.From.UserName,
		Text:     fullText,
	}

	resp, err := t.handler.HandleMessage(msgCtx, chatMsg)
	if err != nil {
		t.sendMessage(chatID, "Something went wrong: "+err.Error())
		return
	}
	t.sendLong(chatID, resp)
}

// ── Model/provider command handlers ─────────────────────────────────────────

func (t *TelegramBot) handleModelCommand(chatID int64, msg *tgbotapi.Message) {
	if t.providerMgr == nil {
		t.sendMessage(chatID, "Provider management not available.")
		return
	}
	info := t.providerMgr.ActiveInfo()
	text := fmt.Sprintf("Provider: %s\nModel: %s", info.Provider, info.Model)

	providers := t.providerMgr.ListProviders()
	var buttons []tgbotapi.InlineKeyboardButton
	for _, p := range providers {
		label := p.Name
		if p.IsActive {
			label = ">> " + label
		}
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(label, "sw:"+p.Name))
	}

	reply := tgbotapi.NewMessage(chatID, text)
	if len(buttons) > 0 {
		reply.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(buttons...),
		)
	}
	reply.ReplyToMessageID = msg.MessageID
	if _, err := t.bot.Send(reply); err != nil {
		log.Error("failed to send model info", "error", err)
	}
}

func (t *TelegramBot) handleModelsCommand(chatID int64, msg *tgbotapi.Message) {
	if t.providerMgr == nil {
		t.sendMessage(chatID, "Provider management not available.")
		return
	}
	providers := t.providerMgr.ListProviders()
	active := t.providerMgr.ActiveInfo()

	var lines []string
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, p := range providers {
		marker := ""
		if p.IsActive {
			marker = " << active"
		}
		keyStatus := ""
		if p.NeedsKey && !p.HasKey {
			keyStatus = " [no API key]"
		}
		lines = append(lines, fmt.Sprintf("\n%s%s%s", p.Name, marker, keyStatus))

		var rowButtons []tgbotapi.InlineKeyboardButton
		for _, m := range p.Models {
			activeMarker := ""
			if p.IsActive && m == active.Model {
				activeMarker = " <<"
			}
			lines = append(lines, fmt.Sprintf("  %s%s", m, activeMarker))

			short := m
			if len(short) > 20 {
				short = short[:20]
			}
			rowButtons = append(rowButtons, tgbotapi.NewInlineKeyboardButtonData(
				short, fmt.Sprintf("sw:%s:%s", p.Name, m),
			))
		}
		for len(rowButtons) > 0 {
			end := 3
			if end > len(rowButtons) {
				end = len(rowButtons)
			}
			rows = append(rows, rowButtons[:end])
			rowButtons = rowButtons[end:]
		}
	}

	text := strings.Join(lines, "\n")
	reply := tgbotapi.NewMessage(chatID, text)
	if len(rows) > 0 {
		reply.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	}
	reply.ReplyToMessageID = msg.MessageID
	if _, err := t.bot.Send(reply); err != nil {
		log.Error("failed to send models list", "error", err)
	}
}

func (t *TelegramBot) handleSwitchCommand(chatID int64, msg *tgbotapi.Message) {
	if t.providerMgr == nil {
		t.sendMessage(chatID, "Provider management not available.")
		return
	}
	args := strings.TrimSpace(msg.CommandArguments())
	if args == "" {
		t.sendMessage(chatID, "Usage: /switch <provider> [model]\nExample: /switch local\nExample: /switch codex\nExample: /switch claude sonnet")
		return
	}
	parts := strings.Fields(args)
	provider, model, ok := t.providerMgr.ResolveTarget(parts[0])
	if !ok {
		t.sendMessage(chatID, fmt.Sprintf("Unknown target: %s\nAvailable: krilllm, local, ollama, codex, claude", parts[0]))
		return
	}
	if len(parts) > 1 {
		model = parts[1]
	}
	if err := t.providerMgr.Switch(provider, model); err != nil {
		t.sendMessage(chatID, fmt.Sprintf("Switch failed: %s", err.Error()))
		return
	}
	info := t.providerMgr.ActiveInfo()
	t.sendMessage(chatID, fmt.Sprintf("Switched to %s (%s)", info.Provider, info.Model))
}

// handleCallbackQuery processes inline keyboard button taps.
func (t *TelegramBot) handleCallbackQuery(query *tgbotapi.CallbackQuery) {
	if query == nil || query.Data == "" {
		return
	}

	callback := tgbotapi.NewCallback(query.ID, "")
	if _, err := t.bot.Request(callback); err != nil {
		log.Debug("callback answer failed", "error", err)
	}

	if t.providerMgr == nil {
		return
	}

	data := query.Data

	if strings.HasPrefix(data, "sw:") {
		parts := strings.SplitN(data[3:], ":", 2)
		provider := parts[0]
		model := ""
		if len(parts) > 1 {
			model = parts[1]
		}

		resolvedProv, resolvedModel, ok := t.providerMgr.ResolveTarget(provider)
		if !ok {
			t.editCallbackMessage(query, fmt.Sprintf("Unknown provider: %s", provider))
			return
		}
		if model != "" {
			resolvedModel = model
		}

		if err := t.providerMgr.Switch(resolvedProv, resolvedModel); err != nil {
			t.editCallbackMessage(query, fmt.Sprintf("Switch failed: %s", err.Error()))
			return
		}

		info := t.providerMgr.ActiveInfo()
		text := fmt.Sprintf("Switched to %s (%s)", info.Provider, info.Model)

		providers := t.providerMgr.ListProviders()
		var buttons []tgbotapi.InlineKeyboardButton
		for _, p := range providers {
			label := p.Name
			if p.Name == info.Provider {
				label = ">> " + label
			}
			buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(label, "sw:"+p.Name))
		}

		edit := tgbotapi.NewEditMessageText(query.Message.Chat.ID, query.Message.MessageID, text)
		if len(buttons) > 0 {
			keyboard := tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(buttons...))
			edit.ReplyMarkup = &keyboard
		}
		if _, err := t.bot.Send(edit); err != nil {
			log.Debug("edit callback message failed", "error", err)
		}
	}
}

func (t *TelegramBot) editCallbackMessage(query *tgbotapi.CallbackQuery, text string) {
	edit := tgbotapi.NewEditMessageText(query.Message.Chat.ID, query.Message.MessageID, text)
	if _, err := t.bot.Send(edit); err != nil {
		log.Debug("edit callback message failed", "error", err)
	}
}

// detectNLSwitch checks if a message is a natural language provider switch.
func detectNLSwitch(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))
	prefixes := []string{"switch to ", "use ", "change to ", "change model to ", "set model "}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			candidate := strings.TrimSpace(lower[len(prefix):])
			candidate = strings.TrimRight(candidate, ".!")
			candidate = strings.SplitN(candidate, " for ", 2)[0]
			return strings.TrimSpace(candidate)
		}
	}
	return ""
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
	if len(msg.Photo) > 0 {
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
	text = sanitizeMarkdown(text)
	chunks := chunkMessage(text, telegramMaxLen)
	for _, chunk := range chunks {
		t.sendMessage(chatID, chunk)
	}
}

// crossPostTarget holds a parsed cross-post directive.
type crossPostTarget struct {
	ChatID string
	Text   string
}

// extractCrossPostDirectives finds and extracts [CROSSPOST:id]...[/CROSSPOST]
// directives from a response. Returns the cleaned text and any cross-post targets.
func extractCrossPostDirectives(text string) (string, []crossPostTarget) {
	var targets []crossPostTarget
	for {
		start := strings.Index(text, "[CROSSPOST:")
		if start == -1 {
			break
		}
		end := strings.Index(text[start:], "[/CROSSPOST]")
		if end == -1 {
			break
		}
		end += start

		// Parse: [CROSSPOST:chatid]message[/CROSSPOST]
		header := text[start : start+len("[CROSSPOST:")] // "[CROSSPOST:"
		_ = header
		idStart := start + len("[CROSSPOST:")
		closeBracket := strings.Index(text[idStart:], "]")
		if closeBracket == -1 {
			break
		}
		chatID := text[idStart : idStart+closeBracket]
		msgStart := idStart + closeBracket + 1
		msg := strings.TrimSpace(text[msgStart:end])

		if chatID != "" && msg != "" {
			targets = append(targets, crossPostTarget{ChatID: chatID, Text: msg})
		}

		// Remove the directive from the text
		text = text[:start] + text[end+len("[/CROSSPOST]"):]
	}
	return strings.TrimSpace(text), targets
}

// scrubCrosspostLiterals removes any residual [CROSSPOST...] / [/CROSSPOST]
// literal that survived extractCrossPostDirectives — e.g. an opening tag with
// no matching close, or a stray closing tag. #22: without this, a malformed
// directive leaks the raw token straight into the user's chat. Returns the
// cleaned text and whether anything was stripped (for logging).
func scrubCrosspostLiterals(text string) (string, bool) {
	if !strings.Contains(text, "[CROSSPOST") && !strings.Contains(text, "[/CROSSPOST") {
		return text, false
	}
	cleaned := text
	// Drop any "[CROSSPOST:...]" header fragment, then any bare close tag.
	for {
		s := strings.Index(cleaned, "[CROSSPOST")
		if s == -1 {
			break
		}
		e := strings.Index(cleaned[s:], "]")
		if e == -1 {
			// Unterminated header — cut from the marker to end of line.
			nl := strings.IndexByte(cleaned[s:], '\n')
			if nl == -1 {
				cleaned = cleaned[:s]
			} else {
				// Consume the broken line's own newline too, else the text
				// before the marker keeps its newline and we emit a blank
				// line ("line one\n\nline three").
				cleaned = cleaned[:s] + cleaned[s+nl+1:]
			}
			continue
		}
		cleaned = cleaned[:s] + cleaned[s+e+1:]
	}
	cleaned = strings.ReplaceAll(cleaned, "[/CROSSPOST]", "")
	// Also drop a bracket-less close fragment (e.g. "[/CROSSPOST" with no
	// trailing ']'); after the full-tag replace above, any residue is the
	// unterminated mirror of the header case handled in the loop.
	cleaned = strings.ReplaceAll(cleaned, "[/CROSSPOST", "")
	return strings.TrimSpace(cleaned), true
}

// SendToChat sends a message to a specific chat ID. Enables cross-channel messaging.
func (t *TelegramBot) SendToChat(chatID int64, text string) {
	t.sendLong(chatID, text)
}

// sanitizeMarkdown cleans up LLM-generated markdown to be safe for Telegram's
// Markdown parser. Ensures formatting markers are balanced and converts
// triple backticks that Telegram Markdown v1 handles poorly.
func sanitizeMarkdown(text string) string {
	text = balanceMarkers(text, '*')
	text = balanceMarkers(text, '_')
	// Convert triple backtick code blocks to single backtick inline code
	text = strings.ReplaceAll(text, "```\n", "\n")
	text = strings.ReplaceAll(text, "\n```", "\n")
	text = strings.ReplaceAll(text, "```", "`")
	return text
}

// balanceMarkers ensures formatting markers appear in pairs.
// If there's an odd number, strip the last unpaired one.
func balanceMarkers(text string, marker byte) string {
	count := 0
	for i := 0; i < len(text); i++ {
		if text[i] == marker {
			count++
		}
	}
	if count%2 == 0 {
		return text // already balanced
	}
	// Remove the last occurrence of the marker to balance
	lastIdx := strings.LastIndexByte(text, marker)
	if lastIdx >= 0 {
		text = text[:lastIdx] + text[lastIdx+1:]
	}
	return text
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
