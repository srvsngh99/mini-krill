package llm

import (
	"context"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// FailoverProvider runs a primary provider and, on error, falls back to a
// secondary one. The colony uses it to run Krill (the local engine) as primary
// with Ollama as the fallback: if Krill is down or erroring, the agent still
// answers via Ollama. A context cancellation (shutdown) is never masked by a
// fallback attempt.
type FailoverProvider struct {
	primary  core.LLMProvider
	fallback core.LLMProvider
}

func NewFailoverProvider(primary, fallback core.LLMProvider) *FailoverProvider {
	return &FailoverProvider{primary: primary, fallback: fallback}
}

func (f *FailoverProvider) Chat(ctx context.Context, messages []core.Message, opts ...core.ChatOption) (*core.Response, error) {
	resp, err := f.primary.Chat(ctx, messages, opts...)
	if err == nil {
		return resp, nil
	}
	if ctx.Err() != nil {
		return nil, err // shutdown/cancel: don't retry, surface it
	}
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
