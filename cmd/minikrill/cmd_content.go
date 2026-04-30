package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/srvsngh99/mini-krill/internal/content"
)

var (
	summarizeTimeout    time.Duration
	webReadTimeout      time.Duration
	webSummarizeTimeout time.Duration
	researchTimeout     time.Duration
)

var summarizeCmd = &cobra.Command{
	Use:   "summarize [file|dir|url]",
	Short: "Summarize a local file, directory, or web page",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		stack, err := initStack(true)
		if err != nil {
			return err
		}
		defer stack.brain.Close()
		ctx, cancel := context.WithTimeout(context.Background(), summarizeTimeout)
		defer cancel()
		docs, err := content.ReadTarget(ctx, args[0])
		if err != nil {
			return err
		}
		out, err := content.Summarize(ctx, stack.llm, docs, "")
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Read and summarize web pages",
}

var webReadCmd = &cobra.Command{
	Use:   "read [url]",
	Short: "Extract readable text from a web page",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), webReadTimeout)
		defer cancel()
		doc, err := content.ReadURL(ctx, args[0])
		if err != nil {
			return err
		}
		fmt.Println(doc.Text)
		return nil
	},
}

var webSummarizeCmd = &cobra.Command{
	Use:   "summarize [url]",
	Short: "Summarize a web page",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		stack, err := initStack(true)
		if err != nil {
			return err
		}
		defer stack.brain.Close()
		ctx, cancel := context.WithTimeout(context.Background(), webSummarizeTimeout)
		defer cancel()
		doc, err := content.ReadURL(ctx, args[0])
		if err != nil {
			return err
		}
		out, err := content.Summarize(ctx, stack.llm, []content.Document{doc}, "")
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

var researchCmd = &cobra.Command{
	Use:   "research [query]",
	Short: "Search the web and summarize findings with sources",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		stack, err := initStack(true)
		if err != nil {
			return err
		}
		defer stack.brain.Close()
		ctx, cancel := context.WithTimeout(context.Background(), researchTimeout)
		defer cancel()
		out, err := content.Research(ctx, stack.llm, strings.Join(args, " "))
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

func init() {
	summarizeCmd.Flags().DurationVar(&summarizeTimeout, "timeout", 2*time.Minute, "max time for fetch and summary")
	webReadCmd.Flags().DurationVar(&webReadTimeout, "timeout", 2*time.Minute, "max time for fetch")
	webSummarizeCmd.Flags().DurationVar(&webSummarizeTimeout, "timeout", 2*time.Minute, "max time for fetch and summary")
	researchCmd.Flags().DurationVar(&researchTimeout, "timeout", 2*time.Minute, "max time for research")
	webCmd.AddCommand(webReadCmd, webSummarizeCmd)
}
