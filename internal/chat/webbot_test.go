package chat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
)

type stubHandler struct {
	gotText atomic.Value // string
	reply   string
}

func (h *stubHandler) HandleMessage(ctx context.Context, msg core.ChatMessage) (string, error) {
	h.gotText.Store(msg.Text)
	return h.reply, nil
}

// WebBot should long-poll the outbox, run the owner message through the handler,
// post the reply, and ack the item (the at-least-once contract).
func TestWebBotDispatchesRepliesAndAcks(t *testing.T) {
	var polls int32
	var posted, ackedID atomic.Value
	ackCh := make(chan struct{}, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/outbox":
			// First poll yields one item; later polls block briefly then return
			// empty, so the loop does not busy-spin during the test.
			if atomic.AddInt32(&polls, 1) == 1 {
				json.NewEncoder(w).Encode(map[string]any{
					"items": []map[string]string{{"id": "w1", "type": "reply", "text": "ping"}},
				})
				return
			}
			time.Sleep(20 * time.Millisecond)
			io.WriteString(w, `{"items":[]}`)
		case "/api/ingest":
			var p map[string]any
			json.NewDecoder(r.Body).Decode(&p)
			if c, _ := p["content"].(string); c != "" && c != "Mini-Krill online on Reef." {
				posted.Store(c)
			}
			io.WriteString(w, `{"ok":true,"id":"r"}`)
		case "/api/ack":
			var b struct {
				IDs []string `json:"ids"`
			}
			json.NewDecoder(r.Body).Decode(&b)
			if len(b.IDs) > 0 {
				ackedID.Store(b.IDs[0])
			}
			io.WriteString(w, `{"ok":true,"acked":1}`)
			select {
			case ackCh <- struct{}{}:
			default:
			}
		}
	}))
	defer srv.Close()
	t.Setenv("REEF_INGEST_URL", srv.URL)
	t.Setenv("REEF_AGENT_TOKEN", "tok")
	t.Setenv("REEF_AGENT_ID", "minikrill")

	h := &stubHandler{reply: "pong"}
	bot := NewWebBot(h)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { bot.Start(ctx); close(done) }()

	select {
	case <-ackCh:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("item was never acked")
	}
	cancel()
	<-done

	if got, _ := h.gotText.Load().(string); got != "ping" {
		t.Fatalf("handler received %q, want %q", got, "ping")
	}
	if got, _ := posted.Load().(string); got != "pong" {
		t.Fatalf("reply posted %q, want %q", got, "pong")
	}
	if got, _ := ackedID.Load().(string); got != "w1" {
		t.Fatalf("acked id %q, want %q", got, "w1")
	}
}
