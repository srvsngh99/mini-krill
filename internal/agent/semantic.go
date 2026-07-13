package agent

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// SemanticMemory is the agent's vector-backed semantic memory: the same store
// the Reef feed watcher writes its engagements into (internal/socialmem).
//
// It is declared here as an INTERFACE, and never as the concrete
// *socialmem.Store, on purpose: internal/socialmem is `//go:build colony`, so
// the PUBLIC Mini-Krill build must not gain a dependency on it. Only the
// colony-only wiring (cmd/minikrill/commands_colony.go) ever calls
// SetSemanticMemory; in the public build a.semantic stays nil and every method
// here is a no-op, exactly as before.
type SemanticMemory interface {
	// RecallBlock returns a prompt-ready block of the agent's own most relevant
	// memories for query, or "" when nothing is relevant (or memory is off /
	// unreachable). Never fatal.
	RecallBlock(ctx context.Context, query string) string
	// Remember stores one memory. Best-effort: implementations swallow errors.
	Remember(ctx context.Context, id, text string, meta map[string]any)
}

// recallTimeout bounds the semantic lookup that runs inline on the chat path.
// Memory is an enhancement, not a dependency: if the embedder or the vector
// service is slow or wedged, the user still gets a reply on time, just without
// the recall block.
const recallTimeout = 6 * time.Second

// SetSemanticMemory wires the colony's semantic memory into the agent. Called
// once at startup (colony builds only), before any chat turn runs.
func (a *KrillAgent) SetSemanticMemory(m SemanticMemory) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.semantic = m
}

// injectSemanticMemory appends the agent's semantically-recalled memories to the
// prompt for this turn, as an additive system message just before the latest
// user message.
//
// WHY THIS BELONGS IN THE CHAT PATH (do not "tidy" it out):
// semantic recall used to be wired into the FEED WATCHER ONLY. The agent could
// therefore remember what it had said in public Reef posts, but had literal
// amnesia about the conversation you had with it directly: the chat prompt
// carried only raw scrollback (recall.go), which is keyword / `/recall` driven
// and never searches by meaning. This call is what lets the agent say "you
// mentioned this last week" in a DM. The feed call sites do NOT cover chat.
//
// The block is ADDITIVE and clearly delimited (it keeps socialmem's own
// "YOUR RELEVANT MEMORY (...)" header), so it never conflicts with the raw
// scrollback the history already carries, and it is capped by the configured
// recall_k (default 4) so it cannot spam the prompt.
func (a *KrillAgent) injectSemanticMemory(ctx context.Context, enriched []core.Message) []core.Message {
	block := a.semanticRecallBlock(ctx)
	if block == "" {
		return enriched
	}
	return insertBeforeLast(enriched, core.Message{Role: "system", Content: block})
}

// semanticRecallBlock recalls memories relevant to the latest user message.
// Returns "" when semantic memory is not wired in (public build), when there is
// no user message to key on, or when the lookup finds/returns nothing.
func (a *KrillAgent) semanticRecallBlock(ctx context.Context) string {
	if a.semantic == nil {
		return ""
	}
	query := strings.TrimSpace(lastUserContent(a.history))
	if query == "" {
		return ""
	}
	rctx, cancel := context.WithTimeout(ctx, recallTimeout)
	defer cancel()
	block := strings.TrimSpace(a.semantic.RecallBlock(rctx, query))
	if block == "" {
		return ""
	}
	log.Debug("semantic memory recalled for chat turn", "query_preview", truncate(query, 50))
	return block
}

// rememberChatTurn persists one side of a chat turn into semantic memory so a
// later conversation can recall it by meaning. Until now ONLY feed engagements
// were persisted, so a DM was never recallable at all - the agent could not
// remember a conversation you had with it directly. Writes are fire-and-forget
// on a background context: a chat reply must never block on (or fail because
// of) the vector store.
func (a *KrillAgent) rememberChatTurn(kind, text string) {
	mem := a.semantic
	if mem == nil || strings.TrimSpace(text) == "" {
		return
	}
	platform := a.platform
	if platform == "" {
		platform = "direct"
	}
	chatID := a.chatID
	id := kind + ":" + platform + ":" + chatID + ":" + strconv.FormatInt(time.Now().UnixNano(), 10)

	var line string
	if kind == "dm_in" {
		line = "In a " + platform + " chat, the user said to me: " + text
	} else {
		line = "In a " + platform + " chat, I replied: " + text
	}
	meta := map[string]any{
		"kind":         kind,
		"surface":      "dm",
		"platform":     platform,
		"chat_id":      chatID,
		"counterparty": "owner",
	}
	go mem.Remember(context.Background(), id, line, meta)
}

// lastUserContent returns the content of the most recent user message in msgs,
// or "" when there is none.
func lastUserContent(msgs []core.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i].Content
		}
	}
	return ""
}
