// Package agent implements the main Krill Agent - the brain's executive function.
// It receives messages, classifies intent, generates plans, gates approval,
// and orchestrates execution. This is where the krill comes alive.
package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// Compile-time interface check - KrillAgent must satisfy core.Agent.
var _ core.Agent = (*KrillAgent)(nil)

// maxHistory caps conversation history to prevent unbounded memory growth.
// Krill molt their exoskeleton to grow - we shed old messages to stay nimble.
const maxHistory = 20

// approvalWords are inputs that greenlight a pending plan.
var approvalWords = map[string]bool{
	"yes":      true,
	"approve":  true,
	"go":       true,
	"do it":    true,
	"lgtm":     true,
	"go ahead": true,
	"y":        true,
	"yep":      true,
	"sure":     true,
	"proceed":  true,
}

// rejectionWords are inputs that scrap a pending plan.
var rejectionWords = map[string]bool{
	"no":     true,
	"reject": true,
	"cancel": true,
	"stop":   true,
	"nah":    true,
	"nope":   true,
	"n":      true,
	"abort":  true,
}

// KrillAgent is the main agent that implements core.Agent.
// It is the executive function of the krill brain - classifying intent,
// planning tasks, gating approval, and orchestrating execution.
type KrillAgent struct {
	llm         core.LLMProvider
	brain       core.Brain
	skills      core.SkillRegistry
	mcp         core.MCPRegistry
	cfg         config.AgentConfig
	history     []core.Message
	pendingPlan *core.Plan
	subMgr      *SubKrillManager
	mu          sync.Mutex
}

// New creates a fresh KrillAgent wired to all subsystems.
// Like a krill larva hatching in the deep ocean - small but ready to grow.
func New(cfg config.AgentConfig, llm core.LLMProvider, brain core.Brain, skills core.SkillRegistry, mcp core.MCPRegistry) *KrillAgent {
	log.Info("krill agent spawning", "name", cfg.Name, "plan_approval", cfg.PlanApproval)
	return &KrillAgent{
		llm:    llm,
		brain:  brain,
		skills: skills,
		mcp:    mcp,
		cfg:    cfg,
		history: []core.Message{
			{Role: "system", Content: brain.SystemPrompt()},
		},
		subMgr: NewSubKrillManager(cfg, llm),
	}
}

// Chat is the main entry point - every user message flows through here.
// It handles pending plan approval, intent classification, and response generation.
func (a *KrillAgent) Chat(ctx context.Context, input string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// --- Phase 1: Check for pending plan approval ---
	if a.pendingPlan != nil {
		return a.handlePendingPlan(ctx, input)
	}

	// --- Phase 2: Record user message ---
	a.appendMessage(core.Message{Role: "user", Content: input})

	// --- Phase 3: Classify intent ---
	intent := a.classifyIntent(ctx, input)
	log.Debug("intent classified", "input_preview", truncate(input, 50), "intent", intent)

	// --- Phase 4: Route based on intent ---
	var response string
	var err error
	switch intent {
	case "TASK":
		response, err = a.handleTask(ctx, input)
	default:
		response, err = a.handleChat(ctx)
	}

	// --- Phase 5: Learn from interaction (adaptive personality) ---
	if err == nil && a.brain.Memory() != nil {
		go a.recordFeedback(context.Background(), input, response)
	}

	return response, err
}

// Plan generates a plan for the given task. Delegates to the planner module.
func (a *KrillAgent) Plan(ctx context.Context, task string) (*core.Plan, error) {
	return GeneratePlan(ctx, task, a.llm, a.skills)
}

// ExecutePlan runs an approved plan through all its steps.
func (a *KrillAgent) ExecutePlan(ctx context.Context, plan *core.Plan) (string, error) {
	return ExecutePlanSteps(ctx, plan, a.llm, a.brain, a.skills)
}

// SpawnKrill launches a focused sub-agent for a specific task.
// Like a krill swarm splitting to cover more ocean territory.
func (a *KrillAgent) SpawnKrill(ctx context.Context, task string) (*core.SubKrill, error) {
	return a.subMgr.Spawn(ctx, task)
}

// handlePendingPlan processes user input when a plan is awaiting approval.
func (a *KrillAgent) handlePendingPlan(ctx context.Context, input string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(input))

	// Check approval
	if approvalWords[normalized] {
		log.Info("plan approved by user", "task", a.pendingPlan.Task)
		a.pendingPlan.Approved = true
		plan := a.pendingPlan
		a.pendingPlan = nil

		result, err := a.ExecutePlan(ctx, plan)
		if err != nil {
			return fmt.Sprintf("This krill hit a reef executing the plan: %v", err), nil
		}

		a.appendMessage(core.Message{Role: "assistant", Content: result})
		return result, nil
	}

	// Check rejection
	if rejectionWords[normalized] {
		log.Info("plan rejected by user", "task", a.pendingPlan.Task)
		a.pendingPlan = nil
		return "Plan scrapped. What else?", nil
	}

	// Neither approval nor rejection - treat as modification request
	log.Debug("ambiguous plan response", "input", normalized)
	return fmt.Sprintf("I have a plan waiting for your call.\n\n%s\nReply with **yes** to approve, **no** to scrap, or describe what you want changed.",
		FormatPlan(a.pendingPlan)), nil
}

// handleTask generates a plan and presents it for approval.
func (a *KrillAgent) handleTask(ctx context.Context, input string) (string, error) {
	plan, err := a.Plan(ctx, input)
	if err != nil {
		log.Error("plan generation failed", "error", err)
		return fmt.Sprintf("This krill's sonar is glitching - could not plan that task: %v", err), nil
	}

	// If auto-approve is enabled (plan_approval = false), execute immediately
	if !a.cfg.PlanApproval {
		log.Info("auto-approve enabled, executing plan immediately", "task", input)
		plan.Approved = true
		result, err := a.ExecutePlan(ctx, plan)
		if err != nil {
			return fmt.Sprintf("This krill hit a reef during auto-execution: %v", err), nil
		}
		a.appendMessage(core.Message{Role: "assistant", Content: result})
		return result, nil
	}

	// Store as pending and present for approval
	a.pendingPlan = plan
	formatted := FormatPlan(plan)
	log.Info("plan generated, awaiting approval", "task", input, "steps", len(plan.Steps))

	a.appendMessage(core.Message{Role: "assistant", Content: formatted})
	return formatted, nil
}

// handleChat generates a conversational response using the LLM with brain enrichment.
// If the user's question looks like it needs current information, the krill
// automatically searches the web first and includes results in context.
func (a *KrillAgent) handleChat(ctx context.Context) (string, error) {
	// Check if the latest user message is about the krill itself
	lastMsg := ""
	if len(a.history) > 0 {
		lastMsg = a.history[len(a.history)-1].Content
	}

	// Self-awareness: detect and invoke self-skills
	if skillName, skillInput := a.detectSelfSkill(lastMsg); skillName != "" {
		if skill, ok := a.skills.Get(skillName); ok {
			log.Info("invoking self-skill", "skill", skillName)
			result, err := skill.Execute(ctx, skillInput, a.llm)
			if err != nil {
				log.Error("self-skill failed", "skill", skillName, "error", err)
			} else if result != "" {
				// For write skills, return result directly (it's already the confirmation)
				if strings.HasPrefix(skillName, "self:tune") ||
					strings.HasPrefix(skillName, "self:configure") ||
					strings.HasPrefix(skillName, "self:evolve") ||
					strings.HasPrefix(skillName, "self:learn") ||
					strings.HasPrefix(skillName, "self:add-skill") ||
					strings.HasPrefix(skillName, "self:heal") ||
					strings.HasPrefix(skillName, "self:reflect") {
					a.appendMessage(core.Message{Role: "assistant", Content: result})
					return result, nil
				}
				// For read skills, inject into LLM context so the krill can discuss it naturally
				enriched := a.brain.EnrichMessages(a.history)
				selfCtx := core.Message{
					Role:    "system",
					Content: "Here is information about yourself that the user is asking about. Use it to respond naturally in first person:\n\n" + result,
				}
				enriched = append(enriched[:len(enriched)-1], selfCtx, enriched[len(enriched)-1])
				resp, err := a.llm.Chat(ctx, enriched)
				if err != nil {
					// Fallback: return raw self-skill output
					a.appendMessage(core.Message{Role: "assistant", Content: result})
					return result, nil
				}
				a.appendMessage(core.Message{Role: "assistant", Content: resp.Content})
				return resp.Content, nil
			}
		}
	}

	enriched := a.brain.EnrichMessages(a.history)

	// If the question likely needs current info, search the web first
	if a.shouldSearch(lastMsg) {
		if searchSkill, ok := a.skills.Get("search"); ok {
			log.Info("auto-searching web for context", "query", lastMsg)
			searchResults, err := searchSkill.Execute(ctx, lastMsg, nil) // raw results, no LLM summary
			if err == nil && searchResults != "" {
				// Inject search results as context before the user message
				searchCtx := core.Message{
					Role:    "system",
					Content: "Here are recent web search results relevant to the user's question. Use them to provide an informed answer:\n\n" + searchResults,
				}
				// Insert before the last user message
				enriched = append(enriched[:len(enriched)-1], searchCtx, enriched[len(enriched)-1])
			}
		}
	}

	resp, err := a.llm.Chat(ctx, enriched)
	if err != nil {
		log.Error("LLM chat failed", "error", err)
		return "This krill's neural link is fuzzy right now. Could you try again?", nil
	}

	a.appendMessage(core.Message{Role: "assistant", Content: resp.Content})
	log.Debug("chat response generated",
		"tokens_in", resp.PromptTokens,
		"tokens_out", resp.CompletionTokens,
		"model", resp.Model,
	)

	return resp.Content, nil
}

// detectSelfSkill checks if the message is about the krill itself and maps
// it to the appropriate self:* skill. Returns ("", "") if not self-referential.
// Krill have compound eyes with 7 visual pigments - these detect self-references.
func (a *KrillAgent) detectSelfSkill(msg string) (skillName, skillInput string) {
	lower := strings.ToLower(msg)

	// Read-only introspection
	selfMap := []struct {
		triggers []string
		skill    string
	}{
		{[]string{"your health", "check yourself", "are you ok", "how are you feeling", "diagnose yourself"}, "self:health"},
		{[]string{"your personality", "who are you", "describe yourself", "about yourself", "your identity", "your traits"}, "self:inspect"},
		{[]string{"your status", "your uptime", "how long have you been", "your vitals"}, "self:status"},
		{[]string{"your memories", "what do you remember", "what have you learned", "your memory"}, "self:memory"},
		{[]string{"your skills", "your capabilities", "what can you do", "your abilities"}, "self:skills"},
		{[]string{"your config", "your settings", "your configuration", "show config"}, "self:config"},
		// Write operations
		{[]string{"tune your", "change temperature", "set temperature", "tune temperature", "set max_token", "tune max_token"}, "self:tune"},
		{[]string{"learn that", "remember that", "remember this", "memorize", "note that"}, "self:learn"},
		{[]string{"evolve your", "update your personality", "change your style", "change your trait", "add trait", "be more", "be less"}, "self:evolve"},
		{[]string{"add a skill", "create a skill", "new skill", "add skill"}, "self:add-skill"},
		{[]string{"heal yourself", "fix yourself", "self heal", "self-heal", "repair yourself"}, "self:heal"},
		{[]string{"switch to ollama", "switch to openai", "switch to anthropic", "switch to google", "auto approve", "require approval", "log level"}, "self:configure"},
		{[]string{"reflect on yourself", "reflect on our conversations", "evolve yourself", "how have i changed you", "what have you learned about me"}, "self:reflect"},
	}

	for _, entry := range selfMap {
		for _, trigger := range entry.triggers {
			if strings.Contains(lower, trigger) {
				return entry.skill, msg
			}
		}
	}
	return "", ""
}

// recordFeedback silently stores interaction signals for adaptive personality evolution.
// Runs in background goroutine - never blocks the response.
func (a *KrillAgent) recordFeedback(_ context.Context, input, response string) {
	lower := strings.ToLower(input)

	// Detect sentiment signals
	var signal string

	// Positive signals
	positives := []string{"thanks", "thank you", "great", "perfect", "awesome", "love it",
		"exactly", "nice", "good job", "well done", "brilliant", "amazing"}
	for _, p := range positives {
		if strings.Contains(lower, p) {
			signal = "positive"
			break
		}
	}

	// Negative signals
	if signal == "" {
		negatives := []string{"no", "wrong", "not what i", "stop", "bad", "terrible",
			"useless", "don't", "incorrect", "nah"}
		for _, n := range negatives {
			if strings.Contains(lower, n) {
				signal = "negative"
				break
			}
		}
	}

	// Engagement signals
	if signal == "" {
		if len(input) > 100 {
			signal = "engaged" // long input = interested
		} else if strings.Contains(input, "?") {
			signal = "curious" // follow-up question
		}
	}

	if signal == "" {
		return // no signal worth recording
	}

	ctx := context.Background()
	key := fmt.Sprintf("feedback_%d", time.Now().UnixMilli())
	entry := core.MemoryEntry{
		Key:        key,
		Value:      fmt.Sprintf("signal:%s | user: %s | krill: %s", signal, truncate(input, 100), truncate(response, 100)),
		Tags:       []string{"personality-feedback", signal},
		CreatedAt:  time.Now(),
		AccessedAt: time.Now(),
	}

	if err := a.brain.Memory().Store(ctx, entry); err != nil {
		log.Debug("failed to store feedback", "error", err)
	}
}

// shouldSearch detects if a message likely needs current web information.
// Krill have photoreceptors that detect light changes - this detects info needs.
func (a *KrillAgent) shouldSearch(msg string) bool {
	lower := strings.ToLower(msg)
	searchTriggers := []string{
		"search for", "look up", "find out", "what is the latest",
		"current news", "recent", "today", "who is", "what happened",
		"search the web", "google", "search online", "look online",
		"what's new", "latest on", "news about", "find me",
	}
	for _, trigger := range searchTriggers {
		if strings.Contains(lower, trigger) {
			return true
		}
	}
	return false
}

// classifyIntent sends the user input to the LLM for TASK vs CHAT classification.
// Defaults to CHAT if the LLM fails - safe default, just like a krill retreating
// to deeper water when the signal is unclear.
func (a *KrillAgent) classifyIntent(ctx context.Context, input string) string {
	prompt := fmt.Sprintf(
		"Classify this message as either TASK or CHAT. "+
			"TASK means the user wants you to do something specific (research, build, analyze, create, find, fix, etc). "+
			"CHAT means casual conversation, questions, or discussion. "+
			"Reply with exactly one word: TASK or CHAT.\n\nMessage: %s", input,
	)

	msgs := []core.Message{
		{Role: "user", Content: prompt},
	}

	resp, err := a.llm.Chat(ctx, msgs, core.WithTemperature(0.0), core.WithMaxTokens(10))
	if err != nil {
		log.Warn("intent classification failed, defaulting to CHAT", "error", err)
		return "CHAT"
	}

	classification := strings.ToUpper(strings.TrimSpace(resp.Content))
	if strings.Contains(classification, "TASK") {
		return "TASK"
	}

	// Default to CHAT - the safe harbor
	return "CHAT"
}

// appendMessage adds a message to history and trims to maxHistory.
// Krill shed their exoskeleton to grow - we shed old messages to stay nimble.
func (a *KrillAgent) appendMessage(msg core.Message) {
	a.history = append(a.history, msg)

	if len(a.history) > maxHistory {
		// Keep the system prompt (index 0) and trim the oldest non-system messages
		trimmed := make([]core.Message, 0, maxHistory)
		trimmed = append(trimmed, a.history[0]) // preserve system prompt
		excess := len(a.history) - maxHistory
		trimmed = append(trimmed, a.history[1+excess:]...)
		a.history = trimmed
	}
}

// truncate shortens a string for log output.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
