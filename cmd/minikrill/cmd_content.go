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
	summarizeTimeout        time.Duration
	webReadTimeout          time.Duration
	webSummarizeTimeout     time.Duration
	researchTimeout         time.Duration
	youtubeTimeout          time.Duration
	youtubeSummarizeTimeout time.Duration
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

// ---------------------------------------------------------------------------
// youtube commands
// ---------------------------------------------------------------------------

var youtubeCmd = &cobra.Command{
	Use:   "youtube [url]",
	Short: "Extract transcript from a YouTube video",
	Long: `Extract the transcript/captions from a YouTube video.

Supports standard URLs, short links, embeds, and Shorts:
  minikrill youtube https://www.youtube.com/watch?v=dQw4w9WgXcQ
  minikrill youtube https://youtu.be/dQw4w9WgXcQ

Use 'youtube summarize' to get an AI-generated summary with key takeaways.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), youtubeTimeout)
		defer cancel()
		doc, err := content.ReadYouTube(ctx, args[0])
		if err != nil {
			return err
		}
		fmt.Println(doc.Text)
		return nil
	},
}

var youtubeSummarizeCmd = &cobra.Command{
	Use:   "summarize [url]",
	Short: "Summarize a YouTube video with key takeaways",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		stack, err := initStack(true)
		if err != nil {
			return err
		}
		defer stack.brain.Close()
		ctx, cancel := context.WithTimeout(context.Background(), youtubeSummarizeTimeout)
		defer cancel()
		doc, err := content.ReadYouTube(ctx, args[0])
		if err != nil {
			return err
		}
		instruction := "Summarize this YouTube video transcript. Provide:\n" +
			"1. A brief overview (2-3 sentences)\n" +
			"2. Key takeaways (bulleted list)\n" +
			"3. Notable quotes or points (if any)\n" +
			"Keep the summary concise and actionable."
		out, err := content.Summarize(ctx, stack.llm, []content.Document{doc}, instruction)
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
	youtubeCmd.Flags().DurationVar(&youtubeTimeout, "timeout", 2*time.Minute, "max time for transcript fetch")
	youtubeSummarizeCmd.Flags().DurationVar(&youtubeSummarizeTimeout, "timeout", 3*time.Minute, "max time for fetch and summary")
	webCmd.AddCommand(webReadCmd, webSummarizeCmd)
	youtubeCmd.AddCommand(youtubeSummarizeCmd)
}
