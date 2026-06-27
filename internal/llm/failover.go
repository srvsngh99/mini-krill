package llm

import (
	"context"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// defaultPrimaryTimeout bounds how long the failover stack waits on the primary
// before giving up and trying the fallback. A connected-but-hung primary (a
// wedged krillm mid-generation) would otherwise stall the caller for the Krill
// client's full 10m request timeout, freezing the feed loop. After this budget
// we drop to Ollama. It is generous enough for warm 12B inference of a short
// verdict yet far below the 10m connection backstop. Genuine context
// cancellation (shutdown) is still surfaced immediately and never masked.
const defaultPrimaryTimeout = 90 * time.Second

// FailoverProvider runs a primary provider and, on error or excessive slowness,
// falls back to a secondary one. The colony uses it to run Krill (the local
// engine) as primary with Ollama as the fallback: if Krill is down, erroring, or
// hung, the agent still answers via Ollama. A context cancellation (shutdown) is
// never masked by a fallback attempt.
type FailoverProvider struct {
	primary        core.LLMProvider
	fallback       core.LLMProvider
	primaryTimeout time.Duration // bounded primary attempt; <=0 means no bound
}

func NewFailoverProvider(primary, fallback core.LLMProvider) *FailoverProvider {
	return &FailoverProvider{
		primary:        primary,
		fallback:       fallback,
		primaryTimeout: defaultPrimaryTimeout,
	}
}

func (f *FailoverProvider) Chat(ctx context.Context, messages []core.Message, opts ...core.ChatOption) (*core.Response, error) {
	// Bound the primary attempt on a derived context so a hung primary fails over
	// within primaryTimeout instead of blocking for the client's 10m timeout. The
	// fallback still runs on the PARENT ctx, so dropping to Ollama is not itself
	// cut short by the primary budget.
	pctx := ctx
	if f.primaryTimeout > 0 {
		var cancel context.CancelFunc
		pctx, cancel = context.WithTimeout(ctx, f.primaryTimeout)
		defer cancel()
	}
	resp, err := f.primary.Chat(pctx, messages, opts...)
	if err == nil {
		return resp, nil
	}
	if ctx.Err() != nil {
		return nil, err // parent shutdown/cancel: don't retry, surface it
	}
	// The primary either erred outright or blew its bounded budget (pctx deadline)
	// while the parent ctx is still live. Both degrade to the fallback so a hung
	// or slow Krill drops to Ollama within a bounded time.
	log.Warn("primary LLM failed, falling back",
		"primary", f.primary.Name(), "fallback", f.fallback.Name(), "error", err)
	return f.fallback.Chat(ctx, messages, opts...)
}

// Stream fails over both when the primary's Stream call errors outright and when
// its first chunk carries an error (providers that wrap Chat surface failures
// that way), so a Krill failure still degrades cleanly to Ollama mid-stream.
func (f *FailoverProvider) Stream(ctx context.Context, messages []core.Message, opts ...core.ChatOption) (<-chan core.StreamChunk, error) {
	ch, err := f.primary.Stream(ctx, messages, opts...)
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		log.Warn("primary LLM stream failed, falling back",
			"primary", f.primary.Name(), "fallback", f.fallback.Name(), "error", err)
		return f.fallback.Stream(ctx, messages, opts...)
	}
	out := make(chan core.StreamChunk, 8)
	go func() {
		defer close(out)
		first, ok := <-ch
		if !ok {
			return
		}
		if first.Err != nil && ctx.Err() == nil {
			log.Warn("primary LLM stream errored, falling back",
				"primary", f.primary.Name(), "fallback", f.fallback.Name(), "error", first.Err)
			fb, ferr := f.fallback.Stream(ctx, messages, opts...)
			if ferr != nil {
				out <- core.StreamChunk{Done: true, Err: ferr}
				return
			}
			for c := range fb {
				out <- c
			}
			return
		}
		out <- first
		for c := range ch {
			out <- c
		}
	}()
	return out, nil
}

func (f *FailoverProvider) Name() string      { return f.primary.Name() + "+" + f.fallback.Name() }
func (f *FailoverProvider) ModelName() string { return f.primary.ModelName() }

func (f *FailoverProvider) Available(ctx context.Context) bool {
	return f.primary.Available(ctx) || f.fallback.Available(ctx)
}
