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

const unifiedConversationChannel = "unified"

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
	channel     string // durable conversation channel; default is unified across interfaces
	history     []core.Message
	pendingPlan *core.Plan
	subMgr      *SubKrillManager
	mu          sync.Mutex
}

// New creates a fresh KrillAgent wired to all subsystems.
// Like a krill larva hatching in the deep ocean - small but ready to grow.
func New(cfg config.AgentConfig, llm core.LLMProvider, brain core.Brain, skills core.SkillRegistry, mcp core.MCPRegistry) *KrillAgent {
	log.Info("krill agent spawning", "name", cfg.Name, "plan_approval", cfg.PlanApproval)

	sysPrompt := brain.SystemPrompt()

	// Cold-start recovery: inject recent conversation from durable storage
	if cs := brain.ConversationStore(); cs != nil {
		if recoveryCtx := buildRecoveryContext(cs, unifiedConversationChannel, cfg.RecoveryTurns); recoveryCtx != "" {
			sysPrompt += "\n\n## Recent Conversation (from last session)\nBelow is your recent conversation. Use it for continuity. Do not mention recovery unless asked:\n" + recoveryCtx
			log.Info("conversation continuity restored", "channel", unifiedConversationChannel)
		}
	}

	return &KrillAgent{
		llm:     llm,
		brain:   brain,
		skills:  skills,
		mcp:     mcp,
		cfg:     cfg,
		channel: unifiedConversationChannel,
		history: []core.Message{
			{Role: "system", Content: sysPrompt},
		},
		subMgr: NewSubKrillManager(cfg, llm),
	}
}

// SetChannel is kept for platform integrations. Mini Krill intentionally ignores
// the requested platform channel and stores all turns in one unified channel so
// users can move between CLI, TUI, Telegram, and Discord without losing context.
func (a *KrillAgent) SetChannel(_ string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.channel = unifiedConversationChannel
}

// Chat is the main entry point - every user message flows through here.
// It handles pending plan approval, intent classification, and response generation.
func (a *KrillAgent) Chat(ctx context.Context, input string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if response, handled := a.handleProviderCommand(input); handled {
		a.saveTurn("user", input)
		a.saveTurn("assistant", response)
		return response, nil
	}

	// --- Phase 1: Check for pending plan approval ---
	if a.pendingPlan != nil {
		return a.handlePendingPlan(ctx, input)
	}

	// --- Phase 2: Record user message ---
	a.appendMessage(core.Message{Role: "user", Content: input})
	a.saveTurn("user", input)
	a.maybeStoreUserPreference(ctx, input)

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

func (a *KrillAgent) handleProviderCommand(input string) (string, bool) {
	text := strings.TrimSpace(input)
	lower := strings.ToLower(text)
	mgr, ok := a.llm.(core.ProviderControl)
	if !ok {
		return "", false
	}

	switch {
	case lower == "/model" || lower == "model" || lower == "current model":
		info := mgr.ActiveInfo()
		return fmt.Sprintf("Provider: %s\nModel: %s", info.Provider, info.Model), true

	case lower == "/models" || lower == "models" || lower == "list models":
		return formatProviders(mgr.ListProviders()), true

	case strings.HasPrefix(lower, "/use ") || strings.HasPrefix(lower, "use ") || strings.HasPrefix(lower, "switch to "):
		target := strings.TrimSpace(text)
		for _, prefix := range []string{"/use ", "use ", "switch to "} {
			if strings.HasPrefix(strings.ToLower(target), prefix) {
				target = strings.TrimSpace(target[len(prefix):])
				break
			}
		}
		if target == "" {
			return "Usage: /use <local|ollama|codex|claude> [model]", true
		}
		parts := strings.Fields(target)
		provider, model, ok := mgr.ResolveTarget(parts[0])
		if !ok {
			return fmt.Sprintf("Unknown provider or model: %s\n\n%s", parts[0], formatProviders(mgr.ListProviders())), true
		}
		if len(parts) > 1 {
			model = parts[1]
		}
		if err := mgr.Switch(provider, model); err != nil {
			return fmt.Sprintf("Switch failed: %s\n\nTry /auth %s, then /use %s.", err.Error(), provider, provider), true
		}
		info := mgr.ActiveInfo()
		return fmt.Sprintf("Switched to %s (%s).", info.Provider, info.Model), true

	case strings.HasPrefix(lower, "/auth") || strings.HasPrefix(lower, "auth "):
		parts := strings.Fields(lower)
		if len(parts) < 2 {
			return "Usage: /auth <codex|claude|ollama>\n\nCodex: run `codex login`\nClaude: run `claude auth login`\nOllama: run `minikrill ollama ensure`", true
		}
		return authInstructions(parts[1]), true
	}

	return "", false
}

func formatProviders(providers []core.ProviderInfo) string {
	var lines []string
	lines = append(lines, "Available providers:")
	for _, p := range providers {
		active := ""
		if p.IsActive {
			active = " [active]"
		}
		auth := "ready"
		if p.NeedsKey && !p.HasKey {
			auth = "needs API key"
		} else if !p.NeedsKey && !p.HasKey && (p.Name == "codex" || p.Name == "claude") {
			auth = "login required"
		}
		models := strings.Join(p.Models, ", ")
		lines = append(lines, fmt.Sprintf("- %s%s: %s (%s)", p.Name, active, models, auth))
	}
	lines = append(lines, "", "Switch with `/use local`, `/use codex`, or `/use claude`.")
	return strings.Join(lines, "\n")
}

func authInstructions(provider string) string {
	switch provider {
	case "codex", "chatgpt":
		return "Codex subscription auth uses the official Codex CLI. Run:\n\n  codex login\n\nThen return here and run `/use codex`."
	case "claude", "claude-code":
		return "Claude subscription auth uses the official Claude Code CLI. Run:\n\n  claude auth login\n\nThen return here and run `/use claude`."
	case "ollama", "local":
		return "Local auth is not needed. To prepare Ollama, run:\n\n  minikrill ollama ensure\n\nThen return here and run `/use local`."
	default:
		return "Unknown auth target. Use `/auth codex`, `/auth claude`, or `/auth ollama`."
	}
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
		a.saveTurn("assistant", result)
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
		a.saveTurn("assistant", result)
		return result, nil
	}

	// Store as pending and present for approval
	a.pendingPlan = plan
	formatted := FormatPlan(plan)
	log.Info("plan generated, awaiting approval", "task", input, "steps", len(plan.Steps))

	a.appendMessage(core.Message{Role: "assistant", Content: formatted})
	a.saveTurn("assistant", formatted)
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
					a.saveTurn("assistant", result)
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
					a.saveTurn("assistant", result)
					return result, nil
				}
				a.appendMessage(core.Message{Role: "assistant", Content: resp.Content})
				a.saveTurn("assistant", resp.Content)
				return resp.Content, nil
			}
		}
	}

	enriched := a.brain.EnrichMessages(a.history)

	// Inject capability awareness so the LLM knows what it can and cannot do
	if a.skills != nil {
		skillList := buildSkillSummary(a.skills)
		capabilityMsg := core.Message{
			Role:    "system",
			Content: "Your available capabilities: " + skillList + ". You CANNOT do anything outside these capabilities. If asked to do something not listed, say so honestly.",
		}
		enriched = append(enriched[:len(enriched)-1], capabilityMsg, enriched[len(enriched)-1])
	}

	if memoryCtx := a.buildUserMemoryContext(ctx, 8); memoryCtx != "" {
		memoryMsg := core.Message{
			Role:    "system",
			Content: "Known user preferences and durable memories. Use these quietly for personalization; do not mention them unless relevant:\n\n" + memoryCtx,
		}
		enriched = append(enriched[:len(enriched)-1], memoryMsg, enriched[len(enriched)-1])
	}

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

	// If the user is asking about previous conversations, inject DM memories
	if a.brain.Memory() != nil && looksLikeRecallRequest(lastMsg) {
		entries, err := a.brain.Memory().Search(context.Background(), "dm_", 5)
		if err == nil && len(entries) > 0 {
			var contextParts []string
			for _, e := range entries {
				contextParts = append(contextParts, e.Value)
			}
			recallMsg := core.Message{
				Role:    "system",
				Content: "Here are your recent conversation memories. Use them if the user asks about past interactions:\n\n" + strings.Join(contextParts, "\n---\n"),
			}
			enriched = append(enriched[:len(enriched)-1], recallMsg, enriched[len(enriched)-1])
		}
	}

	resp, err := a.llm.Chat(ctx, enriched)
	if err != nil {
		log.Error("LLM chat failed", "error", err)
		return "This krill's neural link is fuzzy right now. Could you try again?", nil
	}

	a.appendMessage(core.Message{Role: "assistant", Content: resp.Content})
	a.saveTurn("assistant", resp.Content)
	log.Debug("chat response generated",
		"tokens_in", resp.PromptTokens,
		"tokens_out", resp.CompletionTokens,
		"model", resp.Model,
	)

	return resp.Content, nil
}

func (a *KrillAgent) maybeStoreUserPreference(ctx context.Context, input string) {
	mem := a.brain.Memory()
	if mem == nil {
		return
	}
	text := strings.TrimSpace(input)
	if text == "" {
		return
	}
	lower := strings.ToLower(text)
	for _, explicit := range []string{"remember ", "remember that ", "learn ", "learn that ", "note ", "note that ", "memorize ", "memorize that "} {
		if strings.HasPrefix(lower, explicit) {
			return
		}
	}
	triggers := []string{
		"my name is ",
		"call me ",
		"i prefer ",
		"i like ",
		"i usually ",
		"please always ",
		"please don't ",
		"do not ",
	}
	matched := false
	for _, trigger := range triggers {
		if strings.Contains(lower, trigger) {
			matched = true
			break
		}
	}
	if !matched {
		return
	}

	key := "user_pref_" + memoryKey(text)
	entry := core.MemoryEntry{
		Key:        key,
		Value:      text,
		Tags:       []string{"user-preference", "auto-learned"},
		CreatedAt:  time.Now(),
		AccessedAt: time.Now(),
	}
	if err := mem.Store(ctx, entry); err != nil {
		log.Warn("failed to store user preference", "error", err)
	}
}

func (a *KrillAgent) buildUserMemoryContext(ctx context.Context, limit int) string {
	mem := a.brain.Memory()
	if mem == nil || limit <= 0 {
		return ""
	}
	entries, err := mem.List(ctx)
	if err != nil || len(entries) == 0 {
		return ""
	}
	var lines []string
	for i := len(entries) - 1; i >= 0 && len(lines) < limit; i-- {
		entry := entries[i]
		if !hasAnyTag(entry.Tags, "user-preference", "self-learned") {
			continue
		}
		value := sanitizeMemoryContextValue(entry.Value)
		if value != "" {
			lines = append(lines, "- user memory: "+fmt.Sprintf("%q", value))
		}
	}
	return strings.Join(lines, "\n")
}

func sanitizeMemoryContextValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 240 {
		value = value[:240] + "..."
	}
	return value
}

func hasAnyTag(tags []string, expected ...string) bool {
	for _, tag := range tags {
		for _, want := range expected {
			if tag == want {
				return true
			}
		}
	}
	return false
}

func memoryKey(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	lastUnderscore := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
		if b.Len() >= 48 {
			break
		}
	}
	key := strings.Trim(b.String(), "_")
	if key == "" {
		key = fmt.Sprintf("%d", time.Now().Unix())
	}
	return key
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
		{[]string{"switch to ollama", "switch to codex", "switch to claude", "switch to openai", "switch to anthropic", "switch to google", "auto approve", "require approval", "log level"}, "self:configure"},
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
		if containsWord(lower, p) {
			signal = "positive"
			break
		}
	}

	// Negative signals - real dissatisfaction only
	if signal == "" {
		negatives := []string{"wrong", "terrible", "useless", "bad", "incorrect", "awful", "broken", "stupid"}
		for _, n := range negatives {
			if containsWord(lower, n) {
				signal = "negative"
				break
			}
		}
	}

	// Correction signals - user is correcting, not angry
	if signal == "" {
		corrections := []string{"no", "nah", "nope", "not what i", "that's not right", "don't"}
		for _, c := range corrections {
			if containsWord(lower, c) {
				signal = "correction"
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

// saveTurn persists a single turn to the durable conversation store.
// Runs inline (not goroutine) to ensure ordering.
func (a *KrillAgent) saveTurn(role, content string) {
	if cs := a.brain.ConversationStore(); cs != nil {
		if err := cs.SaveTurn(a.channel, role, content); err != nil {
			log.Debug("failed to save turn", "error", err)
		}
	}
}

// buildRecoveryContext formats recent turns from the conversation store as
// a readable thread for system prompt injection on cold start.
func buildRecoveryContext(store core.ConversationStore, channel string, maxTurns int) string {
	if store == nil || maxTurns <= 0 {
		return ""
	}
	msgs, err := store.LoadRecent(channel, maxTurns)
	if err != nil || len(msgs) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, m := range msgs {
		label := "User"
		if m.Role == "assistant" {
			label = "Krill"
		}
		sb.WriteString(fmt.Sprintf("%s: %s\n", label, truncate(m.Content, 300)))
	}
	return sb.String()
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

// looksLikeRecallRequest detects if the user is asking about previous conversations.
func looksLikeRecallRequest(msg string) bool {
	lower := strings.ToLower(msg)
	triggers := []string{
		"last conversation", "previous conversation", "remember when",
		"do you remember", "we talked about", "earlier we", "last time",
		"our last", "previously", "last chat", "before this",
	}
	for _, t := range triggers {
		if strings.Contains(lower, t) {
			return true
		}
	}
	return false
}

// buildSkillSummary returns a short comma-separated list of enabled skill names
// for injecting into chat context so the LLM knows its actual capabilities.
func buildSkillSummary(skills core.SkillRegistry) string {
	if skills == nil {
		return "chat, plan"
	}
	infos := skills.List()
	if len(infos) == 0 {
		return "chat, plan"
	}
	var parts []string
	for _, info := range infos {
		if info.Enabled {
			parts = append(parts, info.Name)
		}
	}
	if len(parts) == 0 {
		return "chat, plan"
	}
	return "chat, plan, " + strings.Join(parts, ", ")
}

// truncate shortens a string for log output.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// containsWord checks if a word appears as a standalone word in text,
// not as a substring of another word. For example, containsWord("I know", "no")
// returns false, but containsWord("no way", "no") returns true.
func containsWord(text, word string) bool {
	idx := 0
	for {
		pos := strings.Index(text[idx:], word)
		if pos == -1 {
			return false
		}
		absPos := idx + pos
		startOK := absPos == 0 || !isWordChar(text[absPos-1])
		endPos := absPos + len(word)
		endOK := endPos >= len(text) || !isWordChar(text[endPos])
		if startOK && endOK {
			return true
		}
		idx = absPos + 1
		if idx >= len(text) {
			return false
		}
	}
}

// isWordChar returns true if c is a letter, digit, or apostrophe.
func isWordChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '\''
}
