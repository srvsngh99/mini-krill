// Package agent - subagent.go manages sub-agent spawning and lifecycle.
// Sub-agents are lightweight, focused mini-agents that handle specific tasks
// in parallel, like a krill swarm splitting to cover more ocean territory.
package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// defaultPollInterval is how often Wait() checks sub-agent status.
const defaultPollInterval = 250 * time.Millisecond

// SubAgentManager coordinates the lifecycle of spawned sub-agents.
// It enforces concurrency limits and provides status tracking.
type SubAgentManager struct {
	llm       core.LLMProvider
	cfg       config.AgentConfig
	active    map[string]*core.SubAgent
	mu        sync.Mutex
	idCounter int
}

// NewSubAgentManager creates a manager ready to spawn sub-agents.
func NewSubAgentManager(cfg config.AgentConfig, llm core.LLMProvider) *SubAgentManager {
	maxSubs := cfg.MaxSubAgents
	if maxSubs <= 0 {
		maxSubs = 3 // sensible default - a small but capable swarm
	}

	log.Debug("sub-agent manager initialized", "max_sub_agents", maxSubs)

	return &SubAgentManager{
		llm:    llm,
		cfg:    cfg,
		active: make(map[string]*core.SubAgent),
	}
}

// Spawn launches a new sub-agent goroutine for a focused task.
// It enforces the MaxSubAgents concurrency limit to prevent swarm overload.
// Returns immediately with the SubAgent pointer - caller can poll Status or use Wait().
func (m *SubAgentManager) Spawn(ctx context.Context, task string) (*core.SubAgent, error) {
	m.mu.Lock()

	// Enforce concurrency limit - even krill swarms have carrying capacity
	activeCount := m.countRunning()
	maxSubs := m.cfg.MaxSubAgents
	if maxSubs <= 0 {
		maxSubs = 3
	}
	if activeCount >= maxSubs {
		m.mu.Unlock()
		return nil, fmt.Errorf("swarm is at capacity (%d/%d sub-agents active) - wait for one to surface", activeCount, maxSubs)
	}

	// Generate a unique ID for this sub-agent
	m.idCounter++
	id := fmt.Sprintf("krill-%03d", m.idCounter)

	sub := &core.SubAgent{
		ID:     id,
		Task:   task,
		Status: "spawned",
	}

	m.active[id] = sub
	m.mu.Unlock()

	log.Info("sub-agent spawned", "id", id, "task_preview", truncate(task, 60))

	// Launch the sub-agent in its own goroutine - like a krill breaking from the swarm
	go m.execute(ctx, sub)

	return sub, nil
}

// execute runs the sub-agent's task against the LLM. Called as a goroutine.
func (m *SubAgentManager) execute(ctx context.Context, sub *core.SubAgent) {
	m.mu.Lock()
	sub.Status = "running"
	m.mu.Unlock()

	log.Debug("sub-agent diving", "id", sub.ID)

	prompt := fmt.Sprintf(
		"You are a sub-agent, a focused mini-agent. "+
			"Complete this specific task concisely:\n\n%s", sub.Task,
	)

	msgs := []core.Message{
		{Role: "user", Content: prompt},
	}

	resp, err := m.llm.Chat(ctx, msgs, core.WithTemperature(0.5))

	m.mu.Lock()
	defer m.mu.Unlock()

	if err != nil {
		sub.Status = "failed"
		sub.Output = fmt.Sprintf("sub-agent hit a thermal vent: %v", err)
		log.Error("sub-agent failed", "id", sub.ID, "error", err)
		return
	}

	sub.Status = "done"
	sub.Output = resp.Content
	log.Info("sub-agent surfaced with results", "id", sub.ID)
}

// Wait blocks until the specified sub-agent finishes or the timeout expires.
// Returns the final SubAgent state or an error on timeout.
func (m *SubAgentManager) Wait(id string, timeout time.Duration) (*core.SubAgent, error) {
	deadline := time.After(timeout)

	for {
		select {
		case <-deadline:
			m.mu.Lock()
			sub, exists := m.active[id]
			m.mu.Unlock()
			if !exists {
				return nil, fmt.Errorf("sub-agent %q not found in the swarm", id)
			}
			return sub, fmt.Errorf("sub-agent %q timed out after %v (status: %s)", id, timeout, sub.Status)

		default:
			m.mu.Lock()
			sub, exists := m.active[id]
			if !exists {
				m.mu.Unlock()
				return nil, fmt.Errorf("sub-agent %q not found in the swarm", id)
			}

			status := sub.Status
			m.mu.Unlock()

			if status == "done" || status == "failed" {
				return sub, nil
			}

			time.Sleep(defaultPollInterval)
		}
	}
}

// List returns a snapshot of all active sub-agents (spawned, running, done, or failed).
func (m *SubAgentManager) List() []*core.SubAgent {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*core.SubAgent, 0, len(m.active))
	for _, sub := range m.active {
		// Return a copy to avoid data races on the caller's side
		snapshot := &core.SubAgent{
			ID:     sub.ID,
			Task:   sub.Task,
			Status: sub.Status,
			Output: sub.Output,
		}
		result = append(result, snapshot)
	}

	return result
}

// Cleanup removes completed (done or failed) sub-agents from the active map.
// Like krill molting their old exoskeletons - shed what is no longer needed.
func (m *SubAgentManager) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	var cleaned []string
	for id, sub := range m.active {
		if sub.Status == "done" || sub.Status == "failed" {
			cleaned = append(cleaned, id)
			delete(m.active, id)
		}
	}

	if len(cleaned) > 0 {
		log.Debug("sub-agent cleanup complete", "removed", len(cleaned), "remaining", len(m.active))
	}
}

// countRunning returns the number of sub-agents that are spawned or running.
// Must be called with m.mu held.
func (m *SubAgentManager) countRunning() int {
	count := 0
	for _, sub := range m.active {
		if sub.Status == "spawned" || sub.Status == "running" {
			count++
		}
	}
	return count
}
