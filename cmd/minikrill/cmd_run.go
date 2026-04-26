package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var runTimeout time.Duration

var runCmd = &cobra.Command{
	Use:   "run [prompt]",
	Short: "One-shot chat: send a prompt and print the response",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		prompt := strings.Join(args, " ")

		stack, err := initStack(true)
		if err != nil {
			return fmt.Errorf("init stack: %w", err)
		}
		defer stack.brain.Close()

		ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
		defer cancel()

		resp, err := stack.agent.Chat(ctx, prompt)
		if err != nil {
			return fmt.Errorf("chat: %w", err)
		}

		fmt.Println(resp)
		return nil
	},
}

func init() {
	runCmd.Flags().DurationVar(&runTimeout, "timeout", 60*time.Second, "max wait time for the response")
}
