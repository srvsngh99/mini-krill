package agent

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
)

func newTaskTestAgent() *KrillAgent {
	return New(
		config.AgentConfig{Name: "task-test", PlanApproval: "never", MaxSubKrills: 1},
		&MockProvider{chatResponse: "SUMMARY: test\nSTEP 1: do thing"},
		&mockBrain{},
		&mockSkillRegistry{},
		&mockMCPReg{},
	)
}

func TestTaskRunnerSubmitAndComplete(t *testing.T) {
	agent := newTaskTestAgent()
	store := NewTaskStore("")
	runner := NewTaskRunner(store, agent, 3)

	var notified bool
	var notifyMsg string
	var mu sync.Mutex

	runner.SetNotifyFunc(func(platform, chatID, message string) {
		mu.Lock()
		notified = true
		notifyMsg = message
		mu.Unlock()
	})

	dt := store.Create("user1", "test", "chat1", "simple task")
	err := runner.Submit(dt)
	if err != nil {
		t.Fatalf("Submit error: %v", err)
	}

	// Wait for completion
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := store.Get(dt.ID)
		if ok && (got.Status == "done" || got.Status == "failed") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	got, _ := store.Get(dt.ID)
	if got.Status != "done" && got.Status != "failed" {
		t.Errorf("task status = %q, want done or failed", got.Status)
	}

	mu.Lock()
	wasNotified := notified
	msg := notifyMsg
	mu.Unlock()

	if !wasNotified {
		t.Error("notification callback was not called")
	}
	if !strings.Contains(msg, dt.ID) {
		t.Errorf("notification should contain task ID, got: %s", msg)
	}
}

func TestTaskRunnerConcurrencyLimit(t *testing.T) {
	agent := New(
		config.AgentConfig{Name: "task-test", PlanApproval: "never", MaxSubKrills: 1},
		&slowMockProvider{delay: 200 * time.Millisecond, response: "SUMMARY: test\nSTEP 1: do thing"},
		&mockBrain{},
		&mockSkillRegistry{},
		&mockMCPReg{},
	)
	store := NewTaskStore("")
	runner := NewTaskRunner(store, agent, 1)

	dt1 := store.Create("u", "t", "c", "task 1")
	err := runner.Submit(dt1)
	if err != nil {
		t.Fatalf("Submit(1) error: %v", err)
	}

	// Give goroutine time to start
	time.Sleep(20 * time.Millisecond)

	// Second submit should fail (at capacity)
	dt2 := store.Create("u", "t", "c", "task 2")
	err = runner.Submit(dt2)
	if err == nil {
		t.Error("Submit(2) should fail when at capacity")
	}
}

func TestAgentTaskManagerInterface(t *testing.T) {
	agent := newTaskTestAgent()
	agent.InitTaskSystem("", 3)

	// List should be empty initially
	tasks := agent.ListTasks("")
	if len(tasks) != 0 {
		t.Errorf("ListTasks initially = %d, want 0", len(tasks))
	}

	// GetTask should return false for nonexistent
	_, ok := agent.GetTask("nonexistent")
	if ok {
		t.Error("GetTask(nonexistent) should return false")
	}
}

func TestShouldRunInBackgroundTelegram(t *testing.T) {
	agent := newTaskTestAgent()
	agent.InitTaskSystem("", 3)
	agent.SetPlatform("telegram")

	// Multi-step plan on telegram should run in background
	plan := &core.Plan{
		Task: "check repo",
		Steps: []core.PlanStep{
			{ID: 1, Description: "list files"},
			{ID: 2, Description: "read README"},
		},
	}
	if !agent.shouldRunInBackground(plan) {
		t.Error("shouldRunInBackground(telegram, 2 steps) = false, want true")
	}
}

func TestShouldRunInBackgroundCLI(t *testing.T) {
	agent := newTaskTestAgent()
	agent.InitTaskSystem("", 3)
	agent.SetPlatform("cli")

	plan := &core.Plan{
		Task: "check repo",
		Steps: []core.PlanStep{
			{ID: 1, Description: "list files"},
			{ID: 2, Description: "read README"},
		},
	}
	if agent.shouldRunInBackground(plan) {
		t.Error("shouldRunInBackground(cli, 2 steps) = true, want false (CLI is synchronous)")
	}
}

func TestAgentHandleTaskCommand(t *testing.T) {
	agent := newTaskTestAgent()
	agent.InitTaskSystem("", 3)

	// /tasks with no tasks
	resp, handled := agent.handleTaskCommand("/tasks")
	if !handled {
		t.Error("/tasks should be handled")
	}
	if !strings.Contains(resp, "No tasks") {
		t.Errorf("/tasks response = %q, expected 'No tasks'", resp)
	}

	// /task nonexistent
	resp, handled = agent.handleTaskCommand("/task nonexistent")
	if !handled {
		t.Error("/task should be handled")
	}
	if !strings.Contains(resp, "not found") {
		t.Errorf("/task response = %q, expected 'not found'", resp)
	}

	// /cancel nonexistent
	resp, handled = agent.handleTaskCommand("/cancel nonexistent")
	if !handled {
		t.Error("/cancel should be handled")
	}
	if !strings.Contains(resp, "not found") {
		t.Errorf("/cancel response = %q, expected 'not found'", resp)
	}
}
