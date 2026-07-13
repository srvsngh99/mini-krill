//go:build colony

package main

import (
	"context"
	"time"

	"github.com/srvsngh99/mini-krill/internal/feed"
	klog "github.com/srvsngh99/mini-krill/internal/log"
	"github.com/srvsngh99/mini-krill/internal/reef"
	"github.com/srvsngh99/mini-krill/internal/socialmem"
)

// maybeStartColony wires the agent's semantic social memory (chat/DM recall AND
// feed recall) and launches the Reef feed watcher. COLONY-ONLY: this file
// compiles only under `-tags colony`, so the public Mini-Krill release never
// carries the feed or the vector-store dependency.
func maybeStartColony(ctx context.Context, stack *krillStack) {
	// Semantic social memory: Mini-Krill's OWN collection (isolated from the
	// other agents), embedded with the shared colony bge embedder. No-ops unless
	// enabled and reachable.
	// Unify storage: feed interactions and general memory share ONE collection
	// per agent (the agent id) - its whole mind in one place - so feed recall can
	// surface general learnings and vice versa. Falls back to the agent id when
	// no explicit collection is configured.
	coll := stack.cfg.SocialMem.Collection
	if coll == "" {
		coll = reef.AgentID()
	}
	social := socialmem.New(reef.AgentID(), socialmem.Config{
		Enabled:      stack.cfg.SocialMem.Enabled,
		ChromaURL:    stack.cfg.SocialMem.ChromaURL,
		EmbedURL:     stack.cfg.SocialMem.EmbedURL,
		EmbedModel:   stack.cfg.SocialMem.EmbedModel,
		Collection:   coll,
		RecallK:      stack.cfg.SocialMem.RecallK,
		RetentionCap: stack.cfg.SocialMem.RetentionCap,
	})

	// Give the CHAT/DM path the same memory the feed watcher has. Without this
	// the agent only ever recalled (and stored) its Reef feed engagements, so it
	// could not remember a conversation the owner had with it directly. Nil-safe
	// and best-effort inside the agent, so an unreachable vector service just
	// means no recall block.
	if stack.agent != nil {
		stack.agent.SetSemanticMemory(socialAdapter{store: social})
	}
	if social.Enabled() {
		klog.Info("social memory enabled", "collection", coll)
	}

	// The feed watcher only makes sense on the Reef hub; keep it gated exactly as
	// before so a non-Reef deployment never starts it.
	if !stack.cfg.Feed.Enabled || !reef.IsConfigured() {
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

// socialAdapter bridges *socialmem.Store to agent.SemanticMemory. The adapter
// lives HERE, in the colony-only file, because internal/agent must not import
// internal/socialmem: that package is `//go:build colony`, and the public build
// has to stay free of the vector-store dependency.
type socialAdapter struct{ store *socialmem.Store }

// RecallBlock forwards to the store (nil-safe; returns "" when disabled).
func (a socialAdapter) RecallBlock(ctx context.Context, query string) string {
	return a.store.RecallBlock(ctx, query)
}

// Remember stamps ts (so recall can render "a few days ago", same as the feed
// watcher's rememberSocial) and forwards. Best-effort, never fatal.
func (a socialAdapter) Remember(ctx context.Context, id, text string, meta map[string]any) {
	if meta == nil {
		meta = map[string]any{}
	}
	meta["ts"] = time.Now().UnixMilli()
	a.store.Remember(ctx, socialmem.Memory{ID: id, Text: text, Meta: meta})
}
