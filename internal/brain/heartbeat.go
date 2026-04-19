package brain

import (
	"context"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// KrillHeartbeat implements core.Heartbeat, emitting periodic health snapshots.
// Like a real krill's heart beating 140 times per minute, this keeps the system alive
// and lets watchers know everything is swimming smoothly.
type KrillHeartbeat struct {
	interval  time.Duration
	llm       core.LLMProvider
	brainDir  string

	mu        sync.RWMutex
	status    core.HealthStatus
	callbacks []func(core.HealthStatus)
	startTime time.Time

	cancel    context.CancelFunc
	done      chan struct{}
	started   bool
}

// NewHeartbeat creates a new KrillHeartbeat that ticks at the given interval.
// The LLM provider is probed on each beat to report backend availability.
func NewHeartbeat(intervalSec int, llm core.LLMProvider, brainDir string) *KrillHeartbeat {
	if intervalSec <= 0 {
		intervalSec = 30
	}
	return &KrillHeartbeat{
		interval: time.Duration(intervalSec) * time.Second,
		llm:      llm,
		brainDir: brainDir,
		done:     make(chan struct{}),
	}
}

// Start launches the heartbeat goroutine. It emits a HealthStatus on each tick
// and calls all registered OnBeat callbacks. The goroutine runs until the provided
// context is cancelled or Stop() is called.
func (h *KrillHeartbeat) Start(ctx context.Context) error {
	h.mu.Lock()
	if h.started {
		h.mu.Unlock()
		return nil // already beating - idempotent like krill regeneration
	}

	beatCtx, cancel := context.WithCancel(ctx)
	h.cancel = cancel
	h.startTime = time.Now()
	h.started = true
	h.done = make(chan struct{})
	h.mu.Unlock()

	// Emit an initial beat immediately
	h.beat(beatCtx)

	go func() {
		defer close(h.done)
		ticker := time.NewTicker(h.interval)
		defer ticker.Stop()

		log.Info("heartbeat started", "interval", h.interval.String())

		for {
			select {
			case <-beatCtx.Done():
				log.Info("heartbeat stopped")
				return
			case <-ticker.C:
				h.beat(beatCtx)
			}
		}
	}()

	return nil
}

// beat performs a single health check cycle and notifies all callbacks.
func (h *KrillHeartbeat) beat(ctx context.Context) {
	status := h.collectStatus(ctx)

	h.mu.Lock()
	h.status = status
	callbacks := make([]func(core.HealthStatus), len(h.callbacks))
	copy(callbacks, h.callbacks)
	h.mu.Unlock()

	for _, fn := range callbacks {
		fn(status)
	}

	log.Debug("heartbeat tick",
		"uptime", status.Uptime.String(),
		"memory_mb", status.MemoryUsed/(1024*1024),
		"llm", status.LLMStatus,
		"brain_ok", status.BrainOK,
	)
}

// collectStatus gathers all health metrics into a HealthStatus snapshot.
func (h *KrillHeartbeat) collectStatus(ctx context.Context) core.HealthStatus {
	// Memory stats - how much plankton are we consuming?
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// LLM availability check
	llmStatus := "unavailable"
	if h.llm != nil {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if h.llm.Available(checkCtx) {
			llmStatus = "ok"
		}
	} else {
		llmStatus = "not_configured"
	}

	// Brain directory health - can we still reach our memories?
	brainOK := true
	if h.brainDir != "" {
		if _, err := os.Stat(h.brainDir); err != nil {
			brainOK = false
		}
	}

	h.mu.RLock()
	uptime := time.Since(h.startTime)
	h.mu.RUnlock()

	return core.HealthStatus{
		Alive:      true,
		Uptime:     uptime,
		MemoryUsed: memStats.Alloc,
		LLMStatus:  llmStatus,
		BrainOK:    brainOK,
		LastBeat:   time.Now().UTC(),
		Version:    core.Version,
	}
}

// Stop gracefully shuts down the heartbeat goroutine and waits for it to exit.
func (h *KrillHeartbeat) Stop() error {
	h.mu.Lock()
	if !h.started {
		h.mu.Unlock()
		return nil
	}
	h.started = false
	cancel := h.cancel
	done := h.done
	h.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done // wait for goroutine to finish
	}

	log.Info("heartbeat stopped gracefully")
	return nil
}

// Status returns the most recent HealthStatus snapshot.
// Safe to call from any goroutine.
func (h *KrillHeartbeat) Status() core.HealthStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.status
}

// OnBeat registers a callback that fires on every heartbeat tick.
// Callbacks run synchronously in the heartbeat goroutine, so keep them fast.
func (h *KrillHeartbeat) OnBeat(fn func(core.HealthStatus)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.callbacks = append(h.callbacks, fn)
}
