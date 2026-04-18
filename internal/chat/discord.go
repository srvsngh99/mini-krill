package chat

import (
	"context"
	"fmt"
	"math/rand"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// discordMaxLen is Discord's hard limit for a single message.
const discordMaxLen = 2000

// DiscordBot implements core.ChatBot for Discord.
type DiscordBot struct {
	session *discordgo.Session
	handler core.ChatHandler
	cfg     config.DiscordConfig
	done    chan struct{}
}

// NewDiscordBot creates a DiscordBot with the provided config and handler.
// The session is created but not opened - call Start to connect.
func NewDiscordBot(cfg config.DiscordConfig, handler core.ChatHandler) (*DiscordBot, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("discord token is empty - set discord.token in config or KRILL_DISCORD_TOKEN env var")
	}

	session, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("create discord session: %w", err)
	}

	session.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsDirectMessages |
		discordgo.IntentMessageContent

	return &DiscordBot{
		session: session,
		handler: handler,
		cfg:     cfg,
		done:    make(chan struct{}),
	}, nil
}

// Platform returns "discord".
func (d *DiscordBot) Platform() string { return "discord" }

// Start connects to Discord and begins processing messages. It blocks until
// ctx is cancelled or Stop is called.
func (d *DiscordBot) Start(ctx context.Context) error {
	// Register the message handler before opening the session so we never miss
	// a message that arrives right after the connection is established.
	d.session.AddHandler(d.onMessageCreate(ctx))

	if err := d.session.Open(); err != nil {
		close(d.done)
		return fmt.Errorf("open discord session: %w", err)
	}

	log.Info("discord bot connected",
		"user", d.session.State.User.Username,
		"guild_filter", d.cfg.GuildID,
		"channel_filter", d.cfg.ChannelID,
	)

	// Block until told to stop.
	select {
	case <-ctx.Done():
		log.Info("discord bot stopping (context cancelled)")
	case <-d.done:
		log.Info("discord bot stopping (stop signal)")
	}

	if err := d.session.Close(); err != nil {
		log.Error("error closing discord session", "error", err)
		return err
	}

	log.Info("discord bot stopped")
	return nil
}

// Stop signals the bot to shut down gracefully.
func (d *DiscordBot) Stop() error {
	select {
	case <-d.done:
		// Already stopped.
	default:
		close(d.done)
	}
	return nil
}

// onMessageCreate returns a discordgo handler function wired to the bot's
// context and chat handler.
func (d *DiscordBot) onMessageCreate(ctx context.Context) func(s *discordgo.Session, m *discordgo.MessageCreate) {
	return func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// Recover from panics so one bad message never crashes the bot.
		defer func() {
			if r := recover(); r != nil {
				log.Error("recovered panic in discord message handler", "panic", fmt.Sprintf("%v", r))
			}
		}()

		// Never reply to ourselves.
		if m.Author.ID == s.State.User.ID {
			return
		}

		// Guild/channel filter: if configured, only respond in matching contexts.
		if d.cfg.GuildID != "" && m.GuildID != d.cfg.GuildID {
			return
		}
		if d.cfg.ChannelID != "" && m.ChannelID != d.cfg.ChannelID {
			return
		}

		text := strings.TrimSpace(m.Content)
		if text == "" {
			return
		}

		// Check for commands (Discord uses ! prefix by convention).
		if strings.HasPrefix(text, "!") {
			d.handleCommand(s, m, text)
			return
		}

		// Check if this is a DM or a mention - in guilds, only respond to DMs
		// and direct mentions to avoid being noisy.
		isDM := m.GuildID == ""
		isMentioned := false
		for _, u := range m.Mentions {
			if u.ID == s.State.User.ID {
				isMentioned = true
				break
			}
		}

		if !isDM && !isMentioned {
			return
		}

		// Strip the bot mention from the message text if present.
		cleanText := text
		if isMentioned {
			cleanText = strings.ReplaceAll(cleanText, "<@"+s.State.User.ID+">", "")
			cleanText = strings.ReplaceAll(cleanText, "<@!"+s.State.User.ID+">", "")
			cleanText = strings.TrimSpace(cleanText)
		}
		if cleanText == "" {
			cleanText = "hello"
		}

		// Show typing indicator.
		if err := s.ChannelTyping(m.ChannelID); err != nil {
			log.Debug("failed to send typing indicator", "error", err)
		}

		chatMsg := core.ChatMessage{
			Platform: "discord",
			ChatID:   m.ChannelID,
			UserID:   m.Author.ID,
			Username: m.Author.Username,
			Text:     cleanText,
		}

		resp, err := d.handler.HandleMessage(ctx, chatMsg)
		if err != nil {
			log.Error("handler error", "error", err)
			resp = "Bubbles! My handler hit a reef. Try again in a moment."
		}
		if strings.TrimSpace(resp) == "" {
			resp = "..."
		}

		d.sendLong(s, m.ChannelID, resp)
	}
}

// handleCommand dispatches Discord bot commands (prefixed with !).
func (d *DiscordBot) handleCommand(s *discordgo.Session, m *discordgo.MessageCreate, text string) {
	// Extract the command word.
	parts := strings.Fields(text)
	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "!help":
		d.sendLong(s, m.ChannelID, strings.Join([]string{
			"**Mini Krill - Commands:**",
			"",
			"`!help`   - Show this help text",
			"`!status` - Check my vital signs",
			"`!fact`   - Learn something about real krill",
			"`!plan`   - Start a dive plan for a task",
			"",
			"Or just mention me or DM me and I'll do my best!",
		}, "\n"))

	case "!status":
		d.sendLong(s, m.ChannelID, fmt.Sprintf(
			"Swimming along just fine! Mini Krill %s reporting for duty. "+
				"All systems nominal - bioluminescence at full glow.",
			core.Version,
		))

	case "!fact":
		facts := core.KrillFacts
		if len(facts) > 0 {
			d.sendLong(s, m.ChannelID, "Did you know? "+facts[rand.Intn(len(facts))])
		} else {
			d.sendLong(s, m.ChannelID, "I seem to have forgotten all my krill facts. That's alarming.")
		}

	case "!plan":
		d.sendLong(s, m.ChannelID,
			"Tell me what you need done and I'll draft a dive plan for your approval! "+
				"Just describe the task in your next message.")

	default:
		// Unknown command - let it fall through to the regular handler so
		// messages like "!hey krill" still get a response.
	}
}

// sendLong sends a response, splitting it into chunks if it exceeds Discord's
// 2000-character limit.
func (d *DiscordBot) sendLong(s *discordgo.Session, channelID, text string) {
	chunks := chunkMessage(text, discordMaxLen)
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		if _, err := s.ChannelMessageSend(channelID, chunk); err != nil {
			log.Error("failed to send discord message",
				"channel_id", channelID,
				"error", err,
			)
		}
	}
}
