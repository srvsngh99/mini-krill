// Package agent — taskrunner.go executes durable tasks in background goroutines
// so long-running work survives Telegram and CLI timeouts.
package agent

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/srvsngh99/mini-krill/internal/brain"
	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// NotifyFunc is called when a background task finishes to deliver the result
// back to the user on their chat platform.
type NotifyFunc func(platform, chatID, message string)

// TaskRunner manages the execution of durable tasks in background goroutines.
type TaskRunner struct {
	store         *TaskStore
	agent         *KrillAgent
	notifyFn      NotifyFunc
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
		maxConcurrent: int32(maxConcurrent),
	}
}

// SetNotifyFunc sets the callback for delivering results to chat platforms.
func (r *TaskRunner) SetNotifyFunc(fn NotifyFunc) {
	r.notifyFn = fn
}

// Submit queues a task for background execution.
func (r *TaskRunner) Submit(task *DurableTask) error {
	current := atomic.LoadInt32(&r.running)
	if current >= r.maxConcurrent {
		return fmt.Errorf("too many concurrent tasks (%d/%d), try again later", current, r.maxConcurrent)
	}

	atomic.AddInt32(&r.running, 1)
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

// notify sends the result back to the user's chat platform.
func (r *TaskRunner) notify(task *DurableTask, message string) {
	if r.notifyFn == nil {
		return
	}
	r.notifyFn(task.Platform, task.ChatID, message)
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
