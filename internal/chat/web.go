package chat

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
	"github.com/srvsngh99/mini-krill/internal/reef"
)

// maxConcurrentTurns bounds how many owner messages WebBot processes at once.
// The owner is a single human, so concurrency above a small number only means
// a flood of queued messages would spawn unbounded goroutines; this caps it.
const maxConcurrentTurns = 4

// drainGrace bounds how long Stop waits for in-flight dispatches to finish
// posting their replies. It is short so shutdown (and launchd's SIGTERM window)
// is never held hostage by a slow turn; turns past the grace are abandoned at
// process exit. The hub does not redeliver, so this is a best-effort flush.
const drainGrace = 5 * time.Second

// WebBot is the Reef hub ChatBot: it long-polls the Reef outbox for the owner's
// messages, runs each through the shared core.ChatHandler, and posts replies
// back to the Reef chat channel. It is the headless, web-app equivalent of the
// Telegram and Discord bots and satisfies core.ChatBot.
type WebBot struct {
	handler core.ChatHandler
	sem     chan struct{}  // bounds concurrent dispatches
	wg      sync.WaitGroup // tracks in-flight dispatches for graceful drain
}

// NewWebBot creates a WebBot bound to the shared chat handler.
func NewWebBot(handler core.ChatHandler) *WebBot {
	return &WebBot{handler: handler, sem: make(chan struct{}, maxConcurrentTurns)}
}

// Platform identifies this bot for owner-gating and notifier routing.
func (w *WebBot) Platform() string { return "web" }

// Start posts a presence ping then long-polls the Reef outbox until ctx is
// cancelled, dispatching each owner message to the handler. Delivery is
// at-least-once: it polls with ack=1 (the hub leases items rather than consuming
// them), acks each item once its turn finishes, and dedupes redeliveries by id.
// A crash between pulling an item and acking leaves it leased, so the hub
// redelivers it rather than dropping the owner message. On shutdown it waits for
// in-flight dispatches to finish so their replies still post and ack.
func (w *WebBot) Start(ctx context.Context) error {
	if err := reef.PostIngest("chat", "status", "Mini-Krill online on Reef."); err != nil {
		log.Warn("reef startup ping failed", "error", err)
	}
	log.Info("web (reef) bot started", "agent", reef.AgentID())
	seen := newSeenSet(1000)
	for {
		if ctx.Err() != nil {
			return nil
		}
		items, err := reef.PollOutbox(ctx, 25, true)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Warn("reef outbox poll failed", "error", err)
			if !sleepCtx(ctx, 5*time.Second) {
				return nil
			}
			continue
		}
		for _, it := range items {
			// A redelivery of an item we already took: re-ack it, in case our
			// earlier ack was lost (otherwise it redelivers-and-skips forever).
			if !seen.take(it.ID) {
				_ = reef.Ack(ctx, []string{it.ID})
				continue
			}
			text := strings.TrimSpace(it.Text)
			if text == "" {
				_ = reef.Ack(ctx, []string{it.ID}) // consume empty items
				continue
			}
			// Acquire a slot before spawning so a burst of queued owner
			// messages cannot spawn unbounded goroutines. Block (respecting
			// ctx) until a slot frees.
			select {
			case w.sem <- struct{}{}:
			case <-ctx.Done():
				return nil
			}
			w.wg.Add(1)
			go func(id, text string) {
				defer w.wg.Done()
				defer func() { <-w.sem }()
				// Detach from ctx for the turn body so an in-flight reply still
				// posts during shutdown drain, but cap it with turnDeadline so a
				// hung turn cannot leak forever (parity with telegram/discord).
				turnCtx, cancel := context.WithTimeout(context.Background(), turnDeadline)
				defer cancel()
				w.dispatch(turnCtx, text)
				// Ack once handled so the hub stops redelivering. Detached +
				// short timeout so it still completes during shutdown drain.
				ackCtx, ackCancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer ackCancel()
				if err := reef.Ack(ackCtx, []string{id}); err != nil {
					log.Warn("reef ack failed", "error", err)
				}
			}(it.ID, text)
		}
	}
}

// seenSet is a bounded FIFO set of ids for at-least-once dedup. It is touched
// only by the single Start goroutine, so it needs no locking.
type seenSet struct {
	max   int
	ids   map[string]bool
	order []string
}

func newSeenSet(max int) *seenSet {
	return &seenSet{max: max, ids: make(map[string]bool)}
}

// take records id and returns true if it had not been seen before. An empty id
// is always treated as new (never deduped).
func (s *seenSet) take(id string) bool {
	if id == "" {
		return true
	}
	if s.ids[id] {
		return false
	}
	s.ids[id] = true
	s.order = append(s.order, id)
	if len(s.order) > s.max {
		delete(s.ids, s.order[0])
		s.order = s.order[1:]
	}
	return true
}

// dispatch runs one owner message through the handler and posts the reply.
func (w *WebBot) dispatch(ctx context.Context, text string) {
	msg := core.ChatMessage{
		Platform: "web", ChatID: "reef", UserID: "owner",
		Username: "owner", Text: text,
	}
	resp, err := w.handler.HandleMessage(ctx, msg)
	if err != nil {
		log.Error("web handler error", "error", err)
		resp = "I hit an error processing that."
	}
	if strings.TrimSpace(resp) == "" {
		return
	}
	if err := reef.PostIngest("chat", "chat", resp); err != nil {
		log.Error("reef reply post failed", "error", err)
	}
}

// Stop waits up to drainGrace for in-flight dispatches to finish posting their
// replies, then returns. The poll loop itself stops via the Start context, so
// callers should cancel that context before calling Stop. Bounding the wait
// keeps shutdown responsive even if a turn is mid-flight.
func (w *WebBot) Stop() error {
	done := make(chan struct{})
	go func() { w.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(drainGrace):
		log.Warn("reef web bot: drain timed out, some replies may still be in flight")
	}
	return nil
}

// sleepCtx sleeps for d or until ctx is cancelled, returning false if ctx ended.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
