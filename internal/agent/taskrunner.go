// Package agent — taskrunner.go executes durable tasks in background goroutines
// so long-running work survives Telegram and CLI timeouts.
package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/srvsngh99/mini-krill/internal/brain"
	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// NotifyFunc is called when a background task finishes to deliver the result
// back to the user on their chat platform. The platform argument is included
// for legacy single-notifier wiring; per-platform routing should be done via
// RegisterNotifier so each platform's callback only sees its own deliveries.
type NotifyFunc func(platform, chatID, message string)

// PlatformNotifyFunc is the per-platform variant — chatID and message only,
// since platform identity is implicit in the registration.
type PlatformNotifyFunc func(chatID, message string)

// TaskRunner manages the execution of durable tasks in background goroutines.
type TaskRunner struct {
	store         *TaskStore
	agent         *KrillAgent
	notifyMu      sync.RWMutex // guards notifyFn and notifiers
	notifyFn      NotifyFunc
	notifiers     map[string]PlatformNotifyFunc // platform → handler
	maxConcurrent int32
	running       int32 // atomic counter
}

// NewTaskRunner creates a runner wired to a task store and agent.
func NewTaskRunner(store *TaskStore, agent *KrillAgent, maxConcurrent int) *TaskRunner {
	if maxConcurrent <= 0 {
		maxConcurrent = 3
	}
	return &TaskRunner{
		store:         store,
		agent:         agent,
		notifiers:     make(map[string]PlatformNotifyFunc),
		maxConcurrent: int32(maxConcurrent),
	}
}

// SetNotifyFunc sets a single catch-all callback for delivering results to
// chat platforms. Kept for backward compatibility — prefer RegisterNotifier
// when more than one platform is involved.
func (r *TaskRunner) SetNotifyFunc(fn NotifyFunc) {
	r.notifyMu.Lock()
	defer r.notifyMu.Unlock()
	r.notifyFn = fn
}

// RegisterNotifier attaches a per-platform delivery callback. Calls for other
// platforms fall through to the global SetNotifyFunc handler if one is set.
func (r *TaskRunner) RegisterNotifier(platform string, fn PlatformNotifyFunc) {
	r.notifyMu.Lock()
	defer r.notifyMu.Unlock()
	if fn == nil {
		delete(r.notifiers, platform)
		return
	}
	r.notifiers[platform] = fn
}

// Submit queues a task for background execution.
func (r *TaskRunner) Submit(task *DurableTask) error {
	// Add first, then check — avoids TOCTOU race between Load and Add.
	newCount := atomic.AddInt32(&r.running, 1)
	if newCount > r.maxConcurrent {
		atomic.AddInt32(&r.running, -1) // rollback
		return fmt.Errorf("too many concurrent tasks (%d/%d), try again later", newCount-1, r.maxConcurrent)
	}

	go r.run(task)
	return nil
}

// run executes a single task: plan, execute, store result, notify.
func (r *TaskRunner) run(task *DurableTask) {
	defer atomic.AddInt32(&r.running, -1)

	// Create a cancellable context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	r.store.SetCancel(task.ID, cancel)
	r.store.Update(task.ID, "running", "", "")

	log.Info("task runner: starting", "id", task.ID, "task", truncate(task.Task, 60))

	// Generate and execute a plan
	plan, err := r.agent.Plan(ctx, task.Task)
	if err != nil {
		r.store.Update(task.ID, "failed", "", fmt.Sprintf("plan generation failed: %v", err))
		r.notify(task, fmt.Sprintf("Task %s failed during planning: %v", task.ID, err))
		log.Error("task runner: plan failed", "id", task.ID, "error", err)
		return
	}

	plan.Approved = true
	result, err := r.agent.ExecutePlan(ctx, plan)
	if err != nil {
		errMsg := fmt.Sprintf("execution failed: %v", err)
		// Check if cancelled
		if ctx.Err() == context.Canceled {
			r.store.Update(task.ID, "cancelled", result, "cancelled by user")
			r.notify(task, fmt.Sprintf("Task %s was cancelled.", task.ID))
			log.Info("task runner: cancelled", "id", task.ID)
			return
		}
		r.store.Update(task.ID, "failed", result, errMsg)
		r.notify(task, fmt.Sprintf("Task %s failed: %v\n\nPartial results:\n%s", task.ID, err, truncate(result, 2000)))
		log.Error("task runner: execution failed", "id", task.ID, "error", err)
		return
	}

	r.store.Update(task.ID, "done", result, "")
	r.notify(task, fmt.Sprintf("Task %s completed!\n\n%s", task.ID, truncate(result, 3000)))
	log.Info("task runner: completed", "id", task.ID)

	// Store result as a memory entry for future recall
	r.storeAsMemory(task, result)
}

// notify sends the result back to the user's chat platform. Per-platform
// handlers (RegisterNotifier) take precedence; the catch-all NotifyFunc is
// used when no platform-specific handler is registered.
func (r *TaskRunner) notify(task *DurableTask, message string) {
	r.notifyMu.RLock()
	platformFn := r.notifiers[task.Platform]
	fallback := r.notifyFn
	r.notifyMu.RUnlock()

	if platformFn != nil {
		platformFn(task.ChatID, message)
		return
	}
	if fallback != nil {
		fallback(task.Platform, task.ChatID, message)
	}
}

// storeAsMemory saves a task outcome as a durable memory and generates a reflection.
func (r *TaskRunner) storeAsMemory(task *DurableTask, result string) {
	mem := r.agent.brain.Memory()
	if mem == nil {
		return
	}

	key := fmt.Sprintf("task_outcome_%s", task.ID)
	summary := truncate(result, 500)
	entry := core.MemoryEntry{
		Key:        key,
		Value:      fmt.Sprintf("Task: %s\nResult: %s", task.Task, summary),
		Tags:       []string{"task-outcome", task.ID},
		Scope:      "task-outcome",
		Source:     "task-outcome",
		CreatedAt:  time.Now(),
		AccessedAt: time.Now(),
	}

	if err := mem.Store(context.Background(), entry); err != nil {
		log.Debug("failed to store task outcome", "id", task.ID, "error", err)
	}

	// Generate a structured reflection if the memory is a FileMemory
	if fileMem, ok := mem.(*brain.FileMemory); ok {
		consolidator := brain.NewConsolidator(fileMem, r.agent.llm)
		if err := consolidator.ReflectOnTask(context.Background(), result, task.Task); err != nil {
			log.Debug("task reflection failed", "id", task.ID, "error", err)
		}
	}
}
