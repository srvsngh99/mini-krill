package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/spf13/cobra"
)

var notifyCmd = &cobra.Command{
	Use:   "notify [message]",
	Short: "Send a Telegram message via the configured bot",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		token := os.Getenv("KRILL_TELEGRAM_TOKEN")
		if token == "" {
			fmt.Fprintln(os.Stderr, "error: KRILL_TELEGRAM_TOKEN is not set")
			os.Exit(1)
		}

		chatIDStr := os.Getenv("KRILL_TELEGRAM_CHAT_ID")
		if chatIDStr == "" {
			fmt.Fprintln(os.Stderr, "error: KRILL_TELEGRAM_CHAT_ID is not set")
			os.Exit(1)
		}

		chatID, err := strconv.ParseInt(strings.TrimSpace(chatIDStr), 10, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: KRILL_TELEGRAM_CHAT_ID is not a valid integer: %v\n", err)
			os.Exit(1)
		}

		message := strings.Join(args, " ")

		bot, err := tgbotapi.NewBotAPI(token)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: failed to create Telegram bot: %v\n", err)
			os.Exit(1)
		}

		msg := tgbotapi.NewMessage(chatID, message)
		if _, err := bot.Send(msg); err != nil {
			fmt.Fprintf(os.Stderr, "error: failed to send message: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("sent")
		return nil
	},
}
