package llm

import (
	"context"
	"errors"
	"testing"

	"github.com/srvsngh99/mini-krill/internal/core"
)

type fakeProvider struct {
	name  string
	reply string
	err   error
	calls int
}

func (f *fakeProvider) Chat(ctx context.Context, _ []core.Message, _ ...core.ChatOption) (*core.Response, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &core.Response{Content: f.reply, Model: f.name}, nil
}

func (f *fakeProvider) Stream(ctx context.Context, m []core.Message, o ...core.ChatOption) (<-chan core.StreamChunk, error) {
	ch := make(chan core.StreamChunk, 1)
	go func() {
		defer close(ch)
		resp, err := f.Chat(ctx, m, o...)
		if err != nil {
			ch <- core.StreamChunk{Done: true, Err: err}
			return
		}
		ch <- core.StreamChunk{Content: resp.Content, Done: true}
	}()
	return ch, nil
}

func (f *fakeProvider) Name() string                   { return f.name }
func (f *fakeProvider) ModelName() string              { return f.name }
func (f *fakeProvider) Available(context.Context) bool { return f.err == nil }

func TestFailoverUsesPrimaryWhenOK(t *testing.T) {
	p := &fakeProvider{name: "krill", reply: "from-krill"}
	fb := &fakeProvider{name: "ollama", reply: "from-ollama"}
	resp, err := NewFailoverProvider(p, fb).Chat(context.Background(), nil)
	if err != nil || resp.Content != "from-krill" {
		t.Fatalf("want from-krill, got %v err=%v", resp, err)
	}
	if fb.calls != 0 {
		t.Errorf("fallback must not be called when primary succeeds (calls=%d)", fb.calls)
	}
}

func TestFailoverFallsBackOnError(t *testing.T) {
	p := &fakeProvider{name: "krill", err: errors.New("krill down")}
	fb := &fakeProvider{name: "ollama", reply: "from-ollama"}
	resp, err := NewFailoverProvider(p, fb).Chat(context.Background(), nil)
	if err != nil || resp.Content != "from-ollama" {
		t.Fatalf("want from-ollama, got %v err=%v", resp, err)
	}
	if fb.calls != 1 {
		t.Errorf("fallback should be called exactly once (calls=%d)", fb.calls)
	}
}

// A cancelled context (shutdown) must surface the primary error, NOT trigger a
// fallback attempt.
func TestFailoverRespectsCancel(t *testing.T) {
	p := &fakeProvider{name: "krill", err: errors.New("krill down")}
	fb := &fakeProvider{name: "ollama", reply: "from-ollama"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewFailoverProvider(p, fb).Chat(ctx, nil); err == nil {
		t.Fatal("expected primary error to surface on cancel")
	}
	if fb.calls != 0 {
		t.Errorf("must not fall back when ctx is cancelled (calls=%d)", fb.calls)
	}
}

func TestFailoverName(t *testing.T) {
	f := NewFailoverProvider(&fakeProvider{name: "krill"}, &fakeProvider{name: "ollama"})
	if f.Name() != "krill+ollama" {
		t.Errorf("name: got %q", f.Name())
	}
}
