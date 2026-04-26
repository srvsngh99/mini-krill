package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/spf13/cobra"
)

const telegramMaxMessageLen = 4096

var notifyCmd = &cobra.Command{
	Use:   "notify [message]",
	Short: "Send a Telegram message via the configured bot",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		token := os.Getenv("KRILL_TELEGRAM_TOKEN")
		if token == "" {
			return fmt.Errorf("KRILL_TELEGRAM_TOKEN is not set")
		}

		chatIDStr := os.Getenv("KRILL_TELEGRAM_CHAT_ID")
		if chatIDStr == "" {
			return fmt.Errorf("KRILL_TELEGRAM_CHAT_ID is not set")
		}

		chatID, err := strconv.ParseInt(strings.TrimSpace(chatIDStr), 10, 64)
		if err != nil {
			return fmt.Errorf("KRILL_TELEGRAM_CHAT_ID is not a valid integer: %w", err)
		}

		message := strings.Join(args, " ")
		if len(message) > telegramMaxMessageLen {
			return fmt.Errorf("message too long (%d chars); Telegram limit is %d", len(message), telegramMaxMessageLen)
		}

		bot, err := tgbotapi.NewBotAPI(token)
		if err != nil {
			return fmt.Errorf("failed to create Telegram bot: %w", err)
		}

		msg := tgbotapi.NewMessage(chatID, message)
		if _, err := bot.Send(msg); err != nil {
			return fmt.Errorf("failed to send message: %w", err)
		}

		fmt.Println("sent")
		return nil
	},
}
