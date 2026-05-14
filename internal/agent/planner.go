// Package agent - planner.go handles plan generation, formatting, and execution.
// Plans are the krill's navigation chart - they plot the course before diving.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// statusIcons maps plan step statuses to display indicators.
var statusIcons = map[string]string{
	"pending":    "[ ]",
	"running":    "[~]",
	"done":       "[x]",
	"failed":     "[!]",
	"skipped":    "[-]",
	"blocked":    "[?]",
	"need_input": "[?]",
}

// GeneratePlan asks the LLM to decompose a task into concrete, actionable steps.
// The plan is returned unapproved - the user must greenlight it before execution.
func GeneratePlan(ctx context.Context, task string, llm core.LLMProvider, skills core.SkillRegistry, mcp core.MCPRegistry) (*core.Plan, error) {
	// Build the tools inventory for the planner's awareness
	toolbox := NewToolbox(skills, mcp, llm)
	toolDescriptions := toolbox.FormatToolsForLLM()

	prompt := fmt.Sprintf(
		"You are planning a task. Break it into 3-7 concrete steps. "+
			"For each step, write one clear action sentence. "+
			"If a step should use a tool, add [tool:name] at the end of the step. "+
			"For shell commands, use [tool:shell]. "+
			"Format your response as:\n"+
			"SUMMARY: <one-line summary>\n"+
			"STEP 1: <action> [tool:name]\n"+
			"STEP 2: <action>\n"+
			"...\n\n"+
			"Available tools:\n%s\n"+
			"Task: %s", toolDescriptions, task,
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

		// Show output for completed, failed, blocked, or need_input steps.
		// blocked/need_input carry the most user-relevant signal (what's
		// missing) — without them in this list the formatter swallows the
		// "needs the URL" message and shows only a [?] icon.
		if step.Output != "" && (step.Status == "done" || step.Status == "failed" ||
			step.Status == "blocked" || step.Status == "need_input") {
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

// ExecutePlanSteps iterates through each plan step, dispatching real tools when
// possible and falling back to LLM narration when no tool applies.
// Context from previous steps feeds forward so each step builds on the last.
func ExecutePlanSteps(ctx context.Context, plan *core.Plan, llm core.LLMProvider, brain core.Brain, skills core.SkillRegistry, mcp core.MCPRegistry) (string, error) {
	if !plan.Approved {
		return "", fmt.Errorf("this krill refuses to dive without an approved plan")
	}

	log.Info("executing plan", "task", plan.Task, "steps", len(plan.Steps))

	toolbox := NewToolbox(skills, mcp, llm)
	toolDescriptions := toolbox.FormatToolsForLLM()

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
			for j := i + 1; j < len(plan.Steps); j++ {
				plan.Steps[j].Status = "skipped"
			}
			results.WriteString("\nExecution interrupted - the krill surfaced early.\n")
			return results.String(), ctx.Err()
		default:
		}

		step.Status = "running"
		log.Debug("executing step", "step_id", step.ID, "description", truncate(step.Description, 60))

		// Build per-step context. The original user task always leads — the
		// previous bug was that step 1 had no "Previous context" entry, so the
		// LLM hallucinated "no link was provided" even when the link was right
		// there in the user's message. Threading plan.Task ensures every step
		// can see the original input.
		var contextStr string
		var b strings.Builder
		b.WriteString("Original user request:\n")
		b.WriteString(plan.Task)
		b.WriteString("\n\n")
		if len(previousOutputs) > 0 {
			b.WriteString("Previous step outputs:\n")
			b.WriteString(strings.Join(previousOutputs, "\n---\n"))
		} else {
			b.WriteString("This is the first step. Use the original user request above as your input.")
		}
		contextStr = b.String()

		// Try tool-assisted execution: ask LLM what tool to call
		stepOutput, err := executeStepWithTools(ctx, step, contextStr, toolbox, toolDescriptions, llm, brain)

		if err != nil {
			step.Status = "failed"
			step.Output = fmt.Sprintf("Error: %v", err)
			log.Error("step execution failed", "step_id", step.ID, "error", err)
			results.WriteString(fmt.Sprintf("Step %d [!] %s\n  Error: %v\n\n", step.ID, step.Description, err))
			previousOutputs = append(previousOutputs, fmt.Sprintf("Step %d FAILED: %v", step.ID, err))
			continue
		}

		// Honour structured "I can't do this" signals from the LLM. Without
		// these, a step that the LLM admitted it couldn't run was logged as
		// done — the source of the "5/5 steps succeeded" gaslighting.
		switch detectStepBlocker(stepOutput) {
		case "need_input":
			step.Status = "need_input"
			step.Output = stepOutput
			results.WriteString(fmt.Sprintf("Step %d [?] %s\n  Needs input: %s\n\n", step.ID, step.Description, stepOutput))
			previousOutputs = append(previousOutputs, fmt.Sprintf("Step %d NEEDS_INPUT: %s", step.ID, stepOutput))
			log.Info("step needs input", "step_id", step.ID)
			continue
		case "blocked":
			step.Status = "blocked"
			step.Output = stepOutput
			results.WriteString(fmt.Sprintf("Step %d [?] %s\n  Blocked: %s\n\n", step.ID, step.Description, stepOutput))
			previousOutputs = append(previousOutputs, fmt.Sprintf("Step %d BLOCKED: %s", step.ID, stepOutput))
			log.Info("step blocked", "step_id", step.ID)
			continue
		}

		step.Status = "done"
		step.Output = stepOutput
		previousOutputs = append(previousOutputs, fmt.Sprintf("Step %d: %s", step.ID, stepOutput))
		results.WriteString(fmt.Sprintf("Step %d [x] %s\n  %s\n\n", step.ID, step.Description, stepOutput))
		log.Debug("step completed", "step_id", step.ID)
	}

	// Tally results — only "done" counts as success.
	done, failed, blocked := 0, 0, 0
	for _, s := range plan.Steps {
		switch s.Status {
		case "done":
			done++
		case "failed":
			failed++
		case "blocked", "need_input":
			blocked++
		}
	}

	summary := fmt.Sprintf("--- Dive complete: %d/%d steps succeeded", done, len(plan.Steps))
	if failed > 0 {
		summary += fmt.Sprintf(" (%d hit reefs)", failed)
	}
	if blocked > 0 {
		summary += fmt.Sprintf(" (%d need input)", blocked)
	}
	summary += " ---"
	results.WriteString(summary)

	log.Info("plan execution complete", "task", plan.Task, "done", done, "failed", failed, "blocked", blocked)
	return results.String(), nil
}

// detectStepBlocker scans an LLM step output for explicit "I cannot proceed"
// markers. The planner prompt instructs the model to emit these tokens when it
// genuinely can't run the step (missing input, missing tool, etc). Returns
// "need_input", "blocked", or "" (proceed normally).
//
// `NEED_INPUT:` / `BLOCKED:` are the durable contract — substring-matched
// anywhere in the output. The prose heuristics are a safety net for older
// models that don't honour the tokens; they are deliberately anchored to the
// start of the trimmed output (or any line in the first ~5 lines) so a
// legitimate paragraph that quotes a refusal mid-prose ("Draft email: please
// send the user a confirmation") doesn't trip them. A whole-step refusal
// almost always opens with the refusal phrase — that's the shape we want.
func detectStepBlocker(output string) string {
	upper := strings.ToUpper(output)
	if strings.Contains(upper, "NEED_INPUT:") {
		return "need_input"
	}
	if strings.Contains(upper, "BLOCKED:") {
		return "blocked"
	}
	needInputPrefixes := []string{
		"cannot execute this step yet",
		"can't execute this step yet",
		"can't execute this yet",
		"cannot execute this yet",
		"no source material was provided",
		"no url was provided",
		"i still need",
		"please send the video",
		"please send the url",
		"please send the link",
		"send the video link",
		"send the url",
	}
	// Check the first 5 lines: a refusal typically opens the step.
	lines := strings.Split(output, "\n")
	if len(lines) > 5 {
		lines = lines[:5]
	}
	for _, line := range lines {
		l := strings.ToLower(strings.TrimSpace(line))
		for _, p := range needInputPrefixes {
			if strings.HasPrefix(l, p) {
				return "need_input"
			}
		}
	}
	return ""
}

// executeStepWithTools handles a single plan step by asking the LLM if a tool
// should be used. If the LLM responds with a TOOL_CALL directive, the tool is
// dispatched via the toolbox and the real output is fed back to the LLM for
// synthesis. If no tool is called, the LLM's direct response is used.
func executeStepWithTools(ctx context.Context, step *core.PlanStep, contextStr string, toolbox *Toolbox, toolDescriptions string, llm core.LLMProvider, brain core.Brain) (string, error) {
	// If the step already has a tool hint from plan generation, try it directly
	if step.ToolHint != "" {
		toolOutput, err := dispatchToolHint(ctx, step, toolbox)
		if err == nil && toolOutput != "" {
			// Synthesize the raw tool output into a useful summary via LLM
			return synthesizeToolOutput(ctx, step.Description, toolOutput, llm, brain)
		}
		log.Debug("tool hint dispatch failed, falling back to LLM", "tool", step.ToolHint, "error", err)
	}

	// Ask LLM to execute the step, optionally using a tool
	prompt := fmt.Sprintf(
		"Execute this step: %s\n\n%s\n\n"+
			"Available tools:\n%s\n"+
			"If you need to use a tool, respond with EXACTLY this format:\n"+
			"TOOL_CALL: tool_name\n"+
			"ARGS: {\"key\": \"value\"}\n\n"+
			"If no tool is needed, provide your analysis directly.\n\n"+
			"If you genuinely cannot proceed because input or tools are missing, "+
			"respond with one line starting with NEED_INPUT: <what you need> "+
			"or BLOCKED: <why>. Do not fabricate progress.",
		step.Description, contextStr, toolDescriptions,
	)

	msgs := brain.EnrichMessages([]core.Message{
		{Role: "user", Content: prompt},
	})

	resp, err := llm.Chat(ctx, msgs)
	if err != nil {
		return "", err
	}

	// Check if the LLM wants to call a tool
	toolName, toolArgs := parseToolCall(resp.Content)
	if toolName != "" {
		log.Info("LLM requested tool call", "tool", toolName)
		toolOutput, err := toolbox.ExecuteTool(ctx, toolName, toolArgs)
		if err != nil {
			log.Warn("tool call failed, using LLM narration", "tool", toolName, "error", err)
			// Return the error info along with the original LLM response
			return fmt.Sprintf("[tool %s failed: %v]\n%s", toolName, err, cleanToolCallFromResponse(resp.Content)), nil
		}
		// Feed real tool output back to LLM for synthesis
		return synthesizeToolOutput(ctx, step.Description, toolOutput, llm, brain)
	}

	// No tool call — use LLM response directly (narration fallback)
	return resp.Content, nil
}

// dispatchToolHint tries to execute a tool based on the ToolHint from plan generation.
func dispatchToolHint(ctx context.Context, step *core.PlanStep, toolbox *Toolbox) (string, error) {
	args := map[string]string{"input": step.Description}

	// For shell tool, try to extract a command from the step description
	if step.ToolHint == "shell" {
		cmd := extractShellCommand(step.Description)
		if cmd != "" {
			args = map[string]string{"command": cmd}
		} else {
			return "", fmt.Errorf("could not extract shell command from step description")
		}
	}

	return toolbox.ExecuteTool(ctx, step.ToolHint, args)
}

// synthesizeToolOutput sends real tool output back to the LLM for a concise summary.
func synthesizeToolOutput(ctx context.Context, stepDescription, toolOutput string, llm core.LLMProvider, brain core.Brain) (string, error) {
	prompt := fmt.Sprintf(
		"You executed: %s\n\nHere is the real output:\n```\n%s\n```\n\n"+
			"Provide a concise summary of the results. Include key findings.",
		stepDescription, truncate(toolOutput, 4000),
	)

	msgs := brain.EnrichMessages([]core.Message{
		{Role: "user", Content: prompt},
	})

	resp, err := llm.Chat(ctx, msgs)
	if err != nil {
		// If synthesis fails, return raw tool output
		return toolOutput, nil
	}

	return resp.Content, nil
}

// parseToolCall extracts a TOOL_CALL directive and its arguments from LLM output.
// Expected format:
//
//	TOOL_CALL: tool_name
//	ARGS: {"key": "value"}
func parseToolCall(response string) (string, map[string]string) {
	lines := strings.Split(response, "\n")

	var toolName string
	args := make(map[string]string)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(strings.ToUpper(trimmed), "TOOL_CALL:") {
			toolName = strings.TrimSpace(trimmed[len("TOOL_CALL:"):])
			// Look for ARGS on the next line
			if i+1 < len(lines) {
				argsLine := strings.TrimSpace(lines[i+1])
				if strings.HasPrefix(strings.ToUpper(argsLine), "ARGS:") {
					argsStr := strings.TrimSpace(argsLine[len("ARGS:"):])
					args = parseSimpleJSON(argsStr)
				}
			}
			break
		}
	}

	return toolName, args
}

// parseSimpleJSON parses a JSON object string into a string map.
func parseSimpleJSON(s string) map[string]string {
	result := make(map[string]string)
	s = strings.TrimSpace(s)

	// Try proper JSON parse first
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(s), &raw); err == nil {
		for k, v := range raw {
			result[k] = fmt.Sprintf("%v", v)
		}
		return result
	}

	// Fallback: extract key-value pairs from malformed JSON
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	pairs := strings.Split(s, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		colonIdx := strings.Index(pair, ":")
		if colonIdx < 0 {
			continue
		}
		key := strings.Trim(strings.TrimSpace(pair[:colonIdx]), "\"")
		value := strings.Trim(strings.TrimSpace(pair[colonIdx+1:]), "\"")
		if key != "" {
			result[key] = value
		}
	}
	return result
}

// cleanToolCallFromResponse removes the TOOL_CALL/ARGS block from a response
// so we can use the remaining text as narration.
func cleanToolCallFromResponse(response string) string {
	lines := strings.Split(response, "\n")
	var clean []string
	skip := false
	for _, line := range lines {
		upper := strings.ToUpper(strings.TrimSpace(line))
		if strings.HasPrefix(upper, "TOOL_CALL:") {
			skip = true
			continue
		}
		if skip && strings.HasPrefix(upper, "ARGS:") {
			skip = false
			continue
		}
		skip = false
		clean = append(clean, line)
	}
	return strings.TrimSpace(strings.Join(clean, "\n"))
}

// extractShellCommand attempts to extract a shell command from a step description.
// Only honours backtick-quoted commands to avoid extracting garbage from prose.
func extractShellCommand(desc string) string {
	if idx := strings.Index(desc, "`"); idx >= 0 {
		end := strings.Index(desc[idx+1:], "`")
		if end >= 0 {
			cmd := strings.TrimSpace(desc[idx+1 : idx+1+end])
			if cmd != "" {
				return cmd
			}
		}
	}
	return ""
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
				// Extract [tool:name] hint if present
				toolHint := ""
				if tidx := strings.Index(desc, "[tool:"); tidx >= 0 {
					end := strings.Index(desc[tidx:], "]")
					if end >= 0 {
						toolHint = strings.TrimSpace(desc[tidx+len("[tool:") : tidx+end])
						desc = strings.TrimSpace(desc[:tidx] + desc[tidx+end+1:])
					}
				}
				plan.Steps = append(plan.Steps, core.PlanStep{
					ID:          stepCounter,
					Description: desc,
					Status:      "pending",
					ToolHint:    toolHint,
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
