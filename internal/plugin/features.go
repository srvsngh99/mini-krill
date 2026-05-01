package plugin

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/content"
	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
	"github.com/srvsngh99/mini-krill/internal/reminder"
)

// FeatureContext provides dependencies for feature skills that need
// access to state beyond the standard Execute(ctx, input, llm) signature.
type FeatureContext struct {
	Config    *config.Config
	LLM       core.LLMProvider
	Reminders *reminder.Store
}

// NewFeatureSkills creates the feature skills from CLI capabilities.
// Skills that need optional dependencies (reminders, notify) are only
// included when their dependencies are available.
func NewFeatureSkills(fc FeatureContext) []core.Skill {
	skills := []core.Skill{
		youtubeSkill(),
		researchSkill(),
		webSkill(),
	}
	if fc.Reminders != nil {
		skills = append(skills, remindSkill(fc.Reminders))
	}
	// Notify requires Telegram token — check env or config
	if telegramAvailable(fc.Config) {
		skills = append(skills, notifySkill(fc.Config))
	}
	return skills
}

func telegramAvailable(cfg *config.Config) bool {
	if os.Getenv("KRILL_TELEGRAM_TOKEN") != "" {
		return true
	}
	return cfg != nil && cfg.Telegram.Token != ""
}

// ---------------------------------------------------------------------------
// youtube skill
// ---------------------------------------------------------------------------

// urlPattern is a simple regex to extract a URL from user input.
var urlPattern = regexp.MustCompile(`https?://[^\s<>"]+`)

func youtubeSkill() core.Skill {
	return &selfSkill{
		name: "youtube",
		desc: "Summarize or transcribe a YouTube video from its URL",
		exec: func(ctx context.Context, input string, llm core.LLMProvider) (string, error) {
			// Extract URL from input
			url := urlPattern.FindString(input)
			if url == "" {
				return "Please provide a YouTube URL. Example: youtube https://youtube.com/watch?v=...", nil
			}
			if !content.IsYouTubeURL(url) {
				return fmt.Sprintf("%q does not look like a YouTube URL", url), nil
			}

			doc, err := content.ReadYouTube(ctx, url)
			if err != nil {
				return fmt.Sprintf("Could not read YouTube video: %v", err), nil
			}

			if doc.Text == "" {
				return "No transcript or captions available for this video.", nil
			}

			// If LLM available, summarize; otherwise return raw transcript
			if llm != nil {
				summary, err := content.Summarize(ctx, llm, []content.Document{doc},
					"Summarize this YouTube video transcript. Include:\n"+
						"1. Brief overview (2-3 sentences)\n"+
						"2. Key takeaways (bullet points)\n"+
						"3. Notable quotes if any")
				if err == nil && summary != "" {
					return fmt.Sprintf("Video: %s\n\n%s", doc.Source, summary), nil
				}
			}

			// Fallback: return raw transcript (truncated)
			transcript := doc.Text
			if len(transcript) > 3000 {
				transcript = transcript[:3000] + "\n\n... [transcript truncated]"
			}
			return fmt.Sprintf("Video: %s\n\nTranscript:\n%s", doc.Source, transcript), nil
		},
	}
}

// ---------------------------------------------------------------------------
// research skill
// ---------------------------------------------------------------------------

func researchSkill() core.Skill {
	return &selfSkill{
		name: "research",
		desc: "Deep web research with sources and citations",
		exec: func(ctx context.Context, input string, llm core.LLMProvider) (string, error) {
			if strings.TrimSpace(input) == "" {
				return "What should I research? Example: research best Go testing frameworks", nil
			}
			if llm == nil {
				return "Research requires an LLM provider to synthesize findings.", nil
			}
			result, err := content.Research(ctx, llm, input)
			if err != nil {
				return fmt.Sprintf("Research failed: %v", err), nil
			}
			return result, nil
		},
	}
}

// ---------------------------------------------------------------------------
// web skill
// ---------------------------------------------------------------------------

func webSkill() core.Skill {
	return &selfSkill{
		name: "web",
		desc: "Read and summarize any web page from its URL",
		exec: func(ctx context.Context, input string, llm core.LLMProvider) (string, error) {
			url := urlPattern.FindString(input)
			if url == "" {
				return "Please provide a URL. Example: web https://example.com/article", nil
			}

			// If it's a YouTube URL, redirect to youtube skill
			if content.IsYouTubeURL(url) {
				return "This looks like a YouTube URL. Try: youtube " + url, nil
			}

			doc, err := content.ReadURL(ctx, url)
			if err != nil {
				return fmt.Sprintf("Could not read %s: %v", url, err), nil
			}

			if strings.TrimSpace(doc.Text) == "" {
				return fmt.Sprintf("No readable text found at %s", url), nil
			}

			// If LLM available, summarize
			if llm != nil {
				summary, err := content.Summarize(ctx, llm, []content.Document{doc}, "")
				if err == nil && summary != "" {
					return fmt.Sprintf("Source: %s\n\n%s", url, summary), nil
				}
			}

			// Fallback: return raw text (truncated)
			text := doc.Text
			if len(text) > 3000 {
				text = text[:3000] + "\n\n... [content truncated]"
			}
			return fmt.Sprintf("Source: %s\n\n%s", url, text), nil
		},
	}
}

// ---------------------------------------------------------------------------
// remind skill
// ---------------------------------------------------------------------------

func remindSkill(store *reminder.Store) core.Skill {
	return &selfSkill{
		name: "remind",
		desc: "Set, list, and manage reminders with natural language timing",
		exec: func(_ context.Context, input string, _ core.LLMProvider) (string, error) {
			input = strings.TrimSpace(input)
			if input == "" {
				return "Usage:\n" +
					"  remind <text> in <duration>  — set a reminder\n" +
					"  remind list                  — show all reminders\n" +
					"  remind due                   — show due reminders\n" +
					"  remind done <id>             — mark as complete\n" +
					"  remind delete <id>           — delete a reminder\n\n" +
					"Examples:\n" +
					"  remind check the build in 30 minutes\n" +
					"  remind standup tomorrow 9am", nil
			}

			lower := strings.ToLower(input)

			// Subcommand: list
			if lower == "list" || lower == "list all" {
				return formatReminderList(store)
			}

			// Subcommand: due
			if lower == "due" {
				return formatDueReminders(store)
			}

			// Subcommand: done <id>
			if strings.HasPrefix(lower, "done ") {
				id := strings.TrimSpace(input[5:])
				if err := store.MarkDone(id); err != nil {
					return fmt.Sprintf("Could not mark done: %v", err), nil
				}
				return fmt.Sprintf("Reminder %s marked as done.", id), nil
			}

			// Subcommand: delete <id>
			if strings.HasPrefix(lower, "delete ") {
				id := strings.TrimSpace(input[7:])
				if err := store.Delete(id); err != nil {
					return fmt.Sprintf("Could not delete: %v", err), nil
				}
				return fmt.Sprintf("Reminder %s deleted.", id), nil
			}

			// Default: create a new reminder
			text, dueAt, err := reminder.Parse(input, "", time.Now())
			if err != nil {
				return fmt.Sprintf("Could not parse reminder: %v\n\nTry: \"remind check logs in 10 minutes\" or \"remind standup tomorrow 9am\"", err), nil
			}

			r, err := store.Add(text, dueAt)
			if err != nil {
				return fmt.Sprintf("Could not save reminder: %v", err), nil
			}

			return fmt.Sprintf("Reminder set!\n  ID: %s\n  Text: %s\n  Due: %s",
				r.ID, r.Text, r.DueAt.Local().Format("2006-01-02 15:04")), nil
		},
	}
}

func formatReminderList(store *reminder.Store) (string, error) {
	all, err := store.List()
	if err != nil {
		return fmt.Sprintf("Could not list reminders: %v", err), nil
	}
	if len(all) == 0 {
		return "No reminders set.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Reminders (%d):\n\n", len(all)))
	for _, r := range all {
		status := "pending"
		if r.DoneAt != nil {
			status = "done"
		} else if r.FiredAt != nil {
			status = "fired"
		} else if !r.DueAt.After(time.Now().UTC()) {
			status = "DUE"
		}
		sb.WriteString(fmt.Sprintf("  [%s] %s — %s (due %s)\n",
			status, r.ID, r.Text, r.DueAt.Local().Format("Jan 2 15:04")))
	}
	return sb.String(), nil
}

func formatDueReminders(store *reminder.Store) (string, error) {
	due, err := store.Due(time.Now())
	if err != nil {
		return fmt.Sprintf("Could not check due reminders: %v", err), nil
	}
	if len(due) == 0 {
		return "No reminders are due right now.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Due reminders (%d):\n\n", len(due)))
	for _, r := range due {
		sb.WriteString(fmt.Sprintf("  %s — %s (was due %s)\n",
			r.ID, r.Text, r.DueAt.Local().Format("Jan 2 15:04")))
	}
	sb.WriteString("\nUse 'remind done <id>' to mark complete.")
	return sb.String(), nil
}

// ---------------------------------------------------------------------------
// notify skill
// ---------------------------------------------------------------------------

func notifySkill(cfg *config.Config) core.Skill {
	return &selfSkill{
		name: "notify",
		desc: "Send a message via Telegram bot",
		exec: func(_ context.Context, input string, _ core.LLMProvider) (string, error) {
			if strings.TrimSpace(input) == "" {
				return "What message should I send? Example: notify deploy is complete", nil
			}

			token := os.Getenv("KRILL_TELEGRAM_TOKEN")
			if token == "" && cfg != nil {
				token = cfg.Telegram.Token
			}
			if token == "" {
				return "Telegram not configured. Set KRILL_TELEGRAM_TOKEN environment variable.", nil
			}

			chatIDStr := os.Getenv("KRILL_TELEGRAM_CHAT_ID")
			if chatIDStr == "" {
				return "Telegram chat ID not configured. Set KRILL_TELEGRAM_CHAT_ID environment variable.", nil
			}

			chatID, err := strconv.ParseInt(strings.TrimSpace(chatIDStr), 10, 64)
			if err != nil {
				return fmt.Sprintf("Invalid KRILL_TELEGRAM_CHAT_ID: %v", err), nil
			}

			if len(input) > 4096 {
				return fmt.Sprintf("Message too long (%d chars). Telegram limit is 4096.", len(input)), nil
			}

			bot, err := tgbotapi.NewBotAPI(token)
			if err != nil {
				return fmt.Sprintf("Could not connect to Telegram: %v", err), nil
			}

			msg := tgbotapi.NewMessage(chatID, input)
			if _, err := bot.Send(msg); err != nil {
				return fmt.Sprintf("Failed to send: %v", err), nil
			}

			log.Info("telegram notification sent", "chars", len(input))
			return "Message sent to Telegram.", nil
		},
	}
}
