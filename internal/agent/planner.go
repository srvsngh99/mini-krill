// Package agent - planner.go handles plan generation, formatting, and execution.
// Plans are the krill's navigation chart - they plot the course before diving.
//
// Krill fact: krill migrate vertically 600 meters every day with precision.
// Our planner charts the course just as carefully before any task begins.
package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// statusIcons maps plan step statuses to display indicators.
var statusIcons = map[string]string{
	"pending": "[ ]",
	"running": "[~]",
	"done":    "[x]",
	"failed":  "[!]",
	"skipped": "[-]",
}

// GeneratePlan asks the LLM to decompose a task into concrete, actionable steps.
// The plan is returned unapproved - the user must greenlight it before execution.
func GeneratePlan(ctx context.Context, task string, llm core.LLMProvider, skills core.SkillRegistry) (*core.Plan, error) {
	// Build the skills inventory for the planner's awareness
	skillList := buildSkillList(skills)

	prompt := fmt.Sprintf(
		"You are planning a task. Break it into 3-7 concrete steps. "+
			"For each step, write one clear action sentence. "+
			"Available skills: %s. "+
			"Format your response as:\n"+
			"SUMMARY: <one-line summary>\n"+
			"STEP 1: <action>\n"+
			"STEP 2: <action>\n"+
			"...\n\n"+
			"Task: %s", skillList, task,
	)

	msgs := []core.Message{
		{Role: "user", Content: prompt},
	}

	log.Debug("generating plan", "task_preview", truncate(task, 60))

	resp, err := llm.Chat(ctx, msgs, core.WithTemperature(0.3), core.WithMaxTokens(1024))
	if err != nil {
		return nil, fmt.Errorf("this krill's planning sonar failed: %w", err)
	}

	plan := parsePlanResponse(task, resp.Content)
	log.Info("plan generated", "task", task, "steps", len(plan.Steps), "summary", plan.Summary)

	return plan, nil
}

// FormatPlan renders a plan as a human-readable string with status indicators.
//
//	=== DIVE PLAN ===
//	Task: ...
//	Summary: ...
//
//	Steps:
//	  1. [ ] step description
//	  2. [x] step description
//	  ...
//
//	Approve this plan? (yes/no)
func FormatPlan(plan *core.Plan) string {
	var b strings.Builder

	b.WriteString("=== DIVE PLAN ===\n")
	b.WriteString(fmt.Sprintf("Task: %s\n", plan.Task))
	b.WriteString(fmt.Sprintf("Summary: %s\n", plan.Summary))
	b.WriteString("\nSteps:\n")

	for _, step := range plan.Steps {
		icon := statusIcons[step.Status]
		if icon == "" {
			icon = "[ ]"
		}
		b.WriteString(fmt.Sprintf("  %d. %s %s\n", step.ID, icon, step.Description))

		// Show output for completed or failed steps
		if step.Output != "" && (step.Status == "done" || step.Status == "failed") {
			// Indent step output for readability
			lines := strings.Split(step.Output, "\n")
			for _, line := range lines {
				b.WriteString(fmt.Sprintf("       %s\n", line))
			}
		}
	}

	if !plan.Approved {
		b.WriteString("\nApprove this plan? (yes/no)")
	}

	return b.String()
}

// ExecutePlanSteps iterates through each plan step, sending it to the LLM for execution.
// Context from previous steps feeds forward so each step builds on the last.
// Like a krill swarm moving in coordinated waves through the ocean column.
func ExecutePlanSteps(ctx context.Context, plan *core.Plan, llm core.LLMProvider, brain core.Brain, skills core.SkillRegistry) (string, error) {
	if !plan.Approved {
		return "", fmt.Errorf("this krill refuses to dive without an approved plan")
	}

	log.Info("executing plan", "task", plan.Task, "steps", len(plan.Steps))

	var previousOutputs []string
	var results strings.Builder

	results.WriteString(fmt.Sprintf("=== DIVE RESULTS ===\nTask: %s\n\n", plan.Task))

	for i := range plan.Steps {
		step := &plan.Steps[i]

		// Check for context cancellation before each step
		select {
		case <-ctx.Done():
			step.Status = "skipped"
			log.Warn("plan execution cancelled", "step", step.ID, "reason", ctx.Err())
			results.WriteString(fmt.Sprintf("Step %d: SKIPPED (cancelled)\n", step.ID))
			// Mark remaining steps as skipped
			for j := i + 1; j < len(plan.Steps); j++ {
				plan.Steps[j].Status = "skipped"
			}
			results.WriteString("\nExecution interrupted - the krill surfaced early.\n")
			return results.String(), ctx.Err()
		default:
		}

		step.Status = "running"
		log.Debug("executing step", "step_id", step.ID, "description", truncate(step.Description, 60))

		// Build context from previous step outputs
		var contextStr string
		if len(previousOutputs) > 0 {
			contextStr = "Previous context:\n" + strings.Join(previousOutputs, "\n---\n")
		} else {
			contextStr = "This is the first step."
		}

		prompt := fmt.Sprintf(
			"Execute this step: %s\n\n%s\n\nProvide a clear, concise result.",
			step.Description, contextStr,
		)

		msgs := brain.EnrichMessages([]core.Message{
			{Role: "user", Content: prompt},
		})

		resp, err := llm.Chat(ctx, msgs)
		if err != nil {
			step.Status = "failed"
			step.Output = fmt.Sprintf("Error: %v", err)
			log.Error("step execution failed", "step_id", step.ID, "error", err)

			results.WriteString(fmt.Sprintf("Step %d [!] %s\n  Error: %v\n\n", step.ID, step.Description, err))

			// Continue with remaining steps - a krill swarm adapts around obstacles
			previousOutputs = append(previousOutputs, fmt.Sprintf("Step %d FAILED: %v", step.ID, err))
			continue
		}

		step.Status = "done"
		step.Output = resp.Content
		previousOutputs = append(previousOutputs, fmt.Sprintf("Step %d: %s", step.ID, resp.Content))

		results.WriteString(fmt.Sprintf("Step %d [x] %s\n  %s\n\n", step.ID, step.Description, resp.Content))
		log.Debug("step completed", "step_id", step.ID)
	}

	// Tally results
	done, failed := 0, 0
	for _, s := range plan.Steps {
		switch s.Status {
		case "done":
			done++
		case "failed":
			failed++
		}
	}

	summary := fmt.Sprintf("--- Dive complete: %d/%d steps succeeded", done, len(plan.Steps))
	if failed > 0 {
		summary += fmt.Sprintf(" (%d hit reefs)", failed)
	}
	summary += " ---"
	results.WriteString(summary)

	log.Info("plan execution complete", "task", plan.Task, "done", done, "failed", failed)
	return results.String(), nil
}

// parsePlanResponse extracts a structured Plan from the LLM's text response.
func parsePlanResponse(task, response string) *core.Plan {
	plan := &core.Plan{
		Task:     task,
		Approved: false,
	}

	lines := strings.Split(response, "\n")
	stepCounter := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Extract summary (case-insensitive match, slice after the colon)
		if strings.HasPrefix(strings.ToUpper(line), "SUMMARY:") {
			plan.Summary = strings.TrimSpace(line[len("SUMMARY:"):])
			continue
		}

		// Extract steps - match "STEP N:" or just numbered lines
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "STEP ") {
			// Extract everything after "STEP N: "
			colonIdx := strings.Index(line, ":")
			if colonIdx >= 0 {
				stepCounter++
				desc := strings.TrimSpace(line[colonIdx+1:])
				plan.Steps = append(plan.Steps, core.PlanStep{
					ID:          stepCounter,
					Description: desc,
					Status:      "pending",
				})
			}
		}
	}

	// Fallback: if no steps were parsed, create a single step from the response
	if len(plan.Steps) == 0 {
		log.Warn("could not parse plan steps from LLM response, creating fallback step")
		plan.Steps = []core.PlanStep{
			{
				ID:          1,
				Description: task,
				Status:      "pending",
			},
		}
	}

	// Fallback summary
	if plan.Summary == "" {
		plan.Summary = truncate(task, 80)
	}

	return plan
}

// buildSkillList creates a comma-separated inventory of available skills.
func buildSkillList(skills core.SkillRegistry) string {
	if skills == nil {
		return "none"
	}

	infos := skills.List()
	if len(infos) == 0 {
		return "none"
	}

	var parts []string
	for _, info := range infos {
		if info.Enabled {
			parts = append(parts, fmt.Sprintf("%s (%s)", info.Name, info.Description))
		}
	}

	if len(parts) == 0 {
		return "none"
	}

	return strings.Join(parts, ", ")
}
