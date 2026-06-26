//go:build colony

package main

import (
	"context"

	"github.com/srvsngh99/mini-krill/internal/feed"
	klog "github.com/srvsngh99/mini-krill/internal/log"
	"github.com/srvsngh99/mini-krill/internal/reef"
	"github.com/srvsngh99/mini-krill/internal/socialmem"
)

// maybeStartFeed launches the Reef feed watcher and the agent's semantic social
// memory. COLONY-ONLY: this file compiles only under `-tags colony`, so the
// public Mini-Krill release never carries the feed or the Chroma dependency.
func maybeStartFeed(ctx context.Context, stack *krillStack) {
	// Semantic social memory: Mini-Krill's OWN Chroma collection (isolated from
	// the other agents), embedded with the shared colony bge embedder. No-ops
	// unless enabled and reachable.
	social := socialmem.New(reef.AgentID(), socialmem.Config{
		Enabled:    stack.cfg.SocialMem.Enabled,
		ChromaURL:  stack.cfg.SocialMem.ChromaURL,
		EmbedURL:   stack.cfg.SocialMem.EmbedURL,
		EmbedModel: stack.cfg.SocialMem.EmbedModel,
		Collection: stack.cfg.SocialMem.Collection,
		RecallK:    stack.cfg.SocialMem.RecallK,
	})
	if social.Enabled() {
		klog.Info("social memory enabled", "collection", reef.AgentID()+"_social")
	}
	if !stack.cfg.Feed.Enabled {
		return
	}
	fw := feed.NewFeedWatcher(stack.llm, stack.brain, social, stack.cfg.Feed)
	go func() {
		if err := fw.Start(ctx); err != nil {
			klog.Error("feed watcher error", "error", err)
		}
	}()
	klog.Info("feed watcher enabled", "agent", reef.AgentID())
}
