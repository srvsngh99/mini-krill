// Package agent — intent router for smart message classification.
// Replaces the binary TASK/CHAT classifier with a multi-intent router
// that handles common cases deterministically and uses the LLM only
// for genuinely ambiguous inputs.
package agent

import (
	"context"
	"regexp"
	"strings"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// Intent represents the classified purpose of a user message.
type Intent string

const (
	IntentChat      Intent = "CHAT"       // casual conversation, greetings, banter
	IntentAnswer    Intent = "ANSWER"     // factual question needing a direct answer
	IntentDiagnose  Intent = "DIAGNOSE"   // error analysis, debugging, "why did X happen"
	IntentRemember  Intent = "REMEMBER"   // memory store/recall operations
	IntentToolTask  Intent = "TOOL_TASK"  // single-tool invocation (search, time, sysinfo, web)
	IntentLongTask  Intent = "LONG_TASK"  // multi-step work needing a plan
	IntentSelfSkill Intent = "SELF_SKILL" // self:* skill invocations
	IntentCommand   Intent = "COMMAND"    // provider commands (/model, /use, etc.)
)

// RouteResult carries the classified intent along with any hints for dispatch.
type RouteResult struct {
	Intent     Intent
	ToolName   string // for IntentToolTask — which skill to invoke
	SkillName  string // for IntentSelfSkill — which self skill matched
	SkillInput string // for IntentSelfSkill — cleaned input for the skill
}

// IntentRouter classifies user messages into intents using a two-tier approach:
// 1. Deterministic pattern matching (handles ~80% of inputs, zero LLM calls)
// 2. LLM fallback for genuinely ambiguous cases
type IntentRouter struct {
	skills core.SkillRegistry
	llm    core.LLMProvider
}

// NewIntentRouter creates a router wired to the skill registry and LLM.
func NewIntentRouter(skills core.SkillRegistry, llm core.LLMProvider) *IntentRouter {
	return &IntentRouter{skills: skills, llm: llm}
}

// commandExact are commands matched exactly (no arguments).
// Note: /status and /help are handled by platform bots (Telegram, Discord)
// directly in their command dispatch, not by the agent. They are NOT listed
// here so that "status" or "help" in natural language falls through to chat.
var commandExact = []string{
	"/model", "/models", "/tasks",
}

// commandPrefixes are commands that take arguments — matched as prefix+" ".
var commandPrefixes = []string{
	"/use ", "/auth ", "/task ", "/cancel ",
}

// greetings that should always route to chat.
var greetings = map[string]bool{
	"hi": true, "hello": true, "hey": true, "sup": true, "yo": true,
	"what's up": true, "whats up": true, "howdy": true, "good morning": true,
	"good evening": true, "good night": true, "gm": true, "gn": true,
}

// hardActionVerbs strongly imply concrete multi-step work touching real
// systems (filesystem, network, packages, services) or producing concrete
// artifacts (code, files, websites). These bias toward LONG_TASK classification.
//
// Soft verbs ("find", "give", "suggest", "brainstorm") are intentionally
// excluded — alone they signal ideation/explanation, not work, and should
// stay in CHAT/ANSWER. The classifier prompt in classifyWithLLM mirrors
// this distinction for ambiguous inputs.
var hardActionVerbs = []string{
	"deploy", "install", "uninstall", "refactor", "migrate", "configure",
	"set up", "scaffold", "bootstrap", "provision", "rollback", "publish",
	"merge", "rebase", "commit", "push", "pull", "checkout",
	"build", "create", "make", "write", "implement", "develop",
	"fix", "debug", "generate",
}

// memoryStoreTriggers indicate the user wants to store something in memory.
var memoryStoreTriggers = []string{
	"remember that", "remember this", "memorize", "note that",
	"learn that", "learn this", "don't forget",
}

// memoryRecallTriggers indicate the user wants to recall from memory.
var memoryRecallTriggers = []string{
	"what do you remember", "what have you learned", "do you remember",
	"remember when", "what do you know about", "recall",
	"last conversation", "previous conversation", "we talked about",
	"earlier we", "last time", "our last", "previously", "last chat",
	"before this",
}

// memoryForgetTriggers indicate the user wants to forget something.
var memoryForgetTriggers = []string{
	"forget that", "forget about", "delete memory", "remove memory",
}

// diagnosticTriggers indicate an error/debugging question.
var diagnosticTriggers = []string{
	"error", "exception", "stack trace", "traceback", "panic",
	"not working", "broken", "crash", "crashing", "failed", "failing",
	"bug", "issue with", "problem with",
	"404", "500", "502", "503", "timeout",
}

// diagnosticQuestions paired with diagnostic context suggest diagnosis intent.
var diagnosticQuestions = []string{
	"why did", "why is", "why does", "why am i getting",
	"what went wrong", "what's wrong", "how do i fix",
	"what does this error mean", "what causes",
}

// timeTriggers route directly to the time skill.
var timeTriggers = []string{
	"what time", "what's the time", "current time", "what date",
	"what's the date", "what day", "what's today",
}

// searchTriggers route directly to the search skill. The list is intentionally
// generous so phrases people actually type ("find me", "latest news on") get
// caught here instead of falling to the LLM classifier where the model tends
// to invent a "I need permission" response.
var searchTriggers = []string{
	"search for", "search the web", "search online", "look up", "lookup",
	"google", "look online", "find me info", "find me", "find some",
	"news on", "news about", "latest on", "latest news", "any news",
	"any updates on", "what's new with", "whats new with", "what's happening with",
	"digest", "headlines", "recent updates", "recent news",
	"what happened today", "what happened yesterday", "today in",
	"current price of", "price of", "stock price",
}

// freshnessKeywords pair a topic with a time-cue. When both are present (along
// with an externalInfoNouns hit) we route to the search tool automatically —
// the user wants up-to-date info, not whatever the model remembers.
var freshnessKeywords = []string{
	"today", "this week", "this month", "right now", "latest", "current",
	"recent", "just announced", "breaking", "as of now", "live",
}

// externalInfoNouns are nouns that, paired with a freshness keyword, strongly
// suggest the user wants fresh external information (search territory). Without
// one of these the freshness word might be incidental ("the current state of
// my plan", "right now I'm thinking…").
var externalInfoNouns = []string{
	"news", "update", "updates", "release", "version", "price", "stock",
	"announcement", "event", "headlines", "digest", "article", "blog",
	"trend", "trends", "happening",
}

// sysInfoTriggers route directly to the sysinfo skill.
var sysInfoTriggers = []string{
	"system info", "system information", "how much ram", "how much memory",
	"cpu info", "disk space", "os info", "what os",
}

// urlPattern detects URLs in input.
var urlPattern = regexp.MustCompile(`https?://[^\s]+`)

// youtubePattern detects YouTube URLs specifically.
var youtubePattern = regexp.MustCompile(`(?:youtube\.com/watch|youtu\.be/)`)

// selfSkillMap maps trigger phrases to self:* skill names.
// Moved from agent.go detectSelfSkill to centralise routing.
var selfSkillMap = []struct {
	triggers []string
	skill    string
}{
	// Write operations checked FIRST — they are more specific than read triggers
	// (e.g. "evolve your personality" should match self:evolve, not self:inspect)
	{[]string{"tune your", "change temperature", "set temperature", "tune temperature", "set max_token", "tune max_token"}, "self:tune"},
	{[]string{"evolve your", "update your personality", "change your style", "change your trait", "add trait", "be more", "be less"}, "self:evolve"},
	{[]string{"add a skill", "create a skill", "new skill", "add skill"}, "self:add-skill"},
	{[]string{"heal yourself", "fix yourself", "self heal", "self-heal", "repair yourself"}, "self:heal"},
	{[]string{"switch to ollama", "switch to codex", "switch to claude", "switch to openai", "switch to anthropic", "switch to google", "auto approve", "require approval", "log level"}, "self:configure"},
	{[]string{"reflect on yourself", "reflect on our conversations", "evolve yourself", "how have i changed you", "what have you learned about me"}, "self:reflect"},
	{[]string{"consolidate memories", "clean up memories", "merge memories", "deduplicate memories"}, "self:consolidate"},
	// Read-only introspection
	{[]string{"your health", "check yourself", "are you ok", "how are you feeling", "diagnose yourself"}, "self:health"},
	{[]string{"your personality", "who are you", "describe yourself", "about yourself", "your identity", "your traits"}, "self:inspect"},
	{[]string{"your status", "your uptime", "how long have you been", "your vitals"}, "self:status"},
	{[]string{"your memories", "what do you remember", "what have you learned", "your memory"}, "self:memory"},
	{[]string{"your skills", "your capabilities", "what can you do", "your abilities"}, "self:skills"},
	{[]string{"your config", "your settings", "your configuration", "show config"}, "self:config"},
	// Eyes-on-self: read-only access to own code & logs
	{[]string{"check your logs", "show your logs", "tail your logs", "your last error", "your recent errors"}, "self:read-logs"},
	{[]string{"show me your code", "show your code for", "read your source", "your source for", "your implementation of"}, "self:read-code"},
}

// Classify determines the intent of a user message.
// It checks deterministic patterns first, then falls back to LLM classification.
func (r *IntentRouter) Classify(ctx context.Context, input string) RouteResult {
	lower := strings.ToLower(strings.TrimSpace(input))

	// 1. Commands — exact match or prefix with arguments
	for _, cmd := range commandExact {
		if lower == cmd {
			return RouteResult{Intent: IntentCommand}
		}
	}
	for _, prefix := range commandPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return RouteResult{Intent: IntentCommand}
		}
	}

	// 2. Self-skill triggers — map to specific self:* skills
	if skillName, skillInput := detectSelfSkillTrigger(input); skillName != "" {
		return RouteResult{
			Intent:     IntentSelfSkill,
			SkillName:  skillName,
			SkillInput: skillInput,
		}
	}

	// 3. Memory operations — store, recall, or forget
	if matchesAny(lower, memoryStoreTriggers) {
		return RouteResult{Intent: IntentRemember, ToolName: "store"}
	}
	if matchesAny(lower, memoryForgetTriggers) {
		return RouteResult{Intent: IntentRemember, ToolName: "forget"}
	}
	if matchesAny(lower, memoryRecallTriggers) {
		return RouteResult{Intent: IntentRemember, ToolName: "recall"}
	}

	// 4. Direct tool triggers — single skill invocations
	if matchesAny(lower, timeTriggers) {
		return RouteResult{Intent: IntentToolTask, ToolName: "time"}
	}
	if matchesAny(lower, sysInfoTriggers) {
		return RouteResult{Intent: IntentToolTask, ToolName: "sysinfo"}
	}
	if matchesAny(lower, searchTriggers) {
		return RouteResult{Intent: IntentToolTask, ToolName: "search"}
	}
	// Freshness auto-route: a time cue ("today", "latest") AND a noun-phrase
	// suggesting external info nearly always means "go search the web".
	// Catches "find me some AI digest from today" without misfiring on
	// "what's the current state of my plan" or "right now I'm thinking…".
	if matchesAny(lower, freshnessKeywords) && matchesAny(lower, externalInfoNouns) &&
		len(strings.Fields(lower)) >= 3 {
		return RouteResult{Intent: IntentToolTask, ToolName: "search"}
	}
	// URL detection: YouTube vs generic web
	if urlPattern.MatchString(lower) {
		if youtubePattern.MatchString(lower) {
			return RouteResult{Intent: IntentToolTask, ToolName: "youtube"}
		}
		return RouteResult{Intent: IntentToolTask, ToolName: "web"}
	}

	// 5. Diagnostic triggers — error/debugging questions
	// Checked before greeting/chat so "why is it crashing?" isn't short-circuited to CHAT.
	if isDiagnostic(lower) {
		return RouteResult{Intent: IntentDiagnose}
	}

	// 6. Greeting / short chat — no action verbs, short message
	if isGreetingOrSmallTalk(lower) {
		return RouteResult{Intent: IntentChat}
	}

	// 7. LLM fallback for genuinely ambiguous inputs
	return r.classifyWithLLM(ctx, input)
}

// detectSelfSkillTrigger checks if the message matches any self:* skill trigger.
// Extracted from the old detectSelfSkill method on KrillAgent.
func detectSelfSkillTrigger(msg string) (skillName, skillInput string) {
	lower := strings.ToLower(msg)
	for _, entry := range selfSkillMap {
		for _, trigger := range entry.triggers {
			if strings.Contains(lower, trigger) {
				return entry.skill, msg
			}
		}
	}
	return "", ""
}

// isGreetingOrSmallTalk returns true for casual short messages.
// Soft verbs ("find me ideas", "give me suggestions") are allowed through —
// they're conversational asks that should land in CHAT/ANSWER, not LONG_TASK.
func isGreetingOrSmallTalk(lower string) bool {
	// Exact greeting match
	if greetings[lower] {
		return true
	}
	// Short message without HARD action verbs → chat. Soft verbs alone don't
	// disqualify it, so "give me ideas" still routes here instead of being
	// pushed into the LLM classifier where it tends to come back LONG_TASK.
	words := strings.Fields(lower)
	if len(words) <= 6 && !containsAnyWord(lower, hardActionVerbs) {
		// But not if it looks like a question about errors
		if !strings.Contains(lower, "error") && !strings.Contains(lower, "bug") {
			return true
		}
	}
	return false
}

// isDiagnostic checks if the message looks like an error/debugging question.
func isDiagnostic(lower string) bool {
	hasDiagContext := matchesAny(lower, diagnosticTriggers)
	hasDiagQuestion := matchesAny(lower, diagnosticQuestions)

	// Strong signal: explicit diagnostic question
	if hasDiagQuestion {
		return true
	}
	// Moderate signal: diagnostic context + question mark or multi-line (pasted output)
	if hasDiagContext && (strings.Contains(lower, "?") || strings.Count(lower, "\n") > 1) {
		return true
	}
	return false
}

// classifyWithLLM sends the input to the LLM for multi-class classification.
// Only called for genuinely ambiguous inputs (~20% of messages).
func (r *IntentRouter) classifyWithLLM(ctx context.Context, input string) RouteResult {
	prompt := `Classify this message into exactly one category. Reply with ONLY the category name, nothing else.

Categories:
- CHAT: casual conversation, greetings, banter, opinions, "what should we build", "tell me about yourself"
- ANSWER: factual question that needs a direct answer, including ideation/brainstorming asks ("give me ideas", "suggest options", "what are some approaches")
- DIAGNOSE: error analysis, debugging, troubleshooting
- LONG_TASK: multi-step work that actually touches real systems — deploys, installs, migrations, refactors, file changes, scaffolding new projects. Brainstorming, suggesting, listing, explaining, or thinking are NOT long tasks.
- TOOL_TASK: single action (search, look up, check time, get system info, fetch a URL)
- REMEMBER: memory operations (remember, recall, forget)

Important: ideation, advice, recommendations, and explanations are CHAT or ANSWER, never LONG_TASK. LONG_TASK requires the user to want something built or changed in their environment.

Message: ` + input

	msgs := []core.Message{
		{Role: "user", Content: prompt},
	}

	resp, err := r.llm.Chat(ctx, msgs, core.WithTemperature(0.0), core.WithMaxTokens(10))
	if err != nil {
		log.Warn("LLM intent classification failed, defaulting to ANSWER", "error", err)
		return RouteResult{Intent: IntentAnswer}
	}

	classification := strings.ToUpper(strings.TrimSpace(resp.Content))
	log.Debug("LLM classified intent", "raw", resp.Content, "parsed", classification)

	switch {
	case strings.Contains(classification, "LONG_TASK"):
		return RouteResult{Intent: IntentLongTask}
	case strings.Contains(classification, "TOOL_TASK"):
		return RouteResult{Intent: IntentToolTask}
	case strings.Contains(classification, "DIAGNOSE"):
		return RouteResult{Intent: IntentDiagnose}
	case strings.Contains(classification, "REMEMBER"):
		return RouteResult{Intent: IntentRemember}
	case strings.Contains(classification, "CHAT"):
		return RouteResult{Intent: IntentChat}
	case strings.Contains(classification, "ANSWER"):
		return RouteResult{Intent: IntentAnswer}
	// Legacy compat: if LLM still says TASK, map to LONG_TASK
	case strings.Contains(classification, "TASK"):
		return RouteResult{Intent: IntentLongTask}
	default:
		return RouteResult{Intent: IntentAnswer}
	}
}

// matchesAny returns true if text contains any of the trigger phrases.
func matchesAny(text string, triggers []string) bool {
	for _, t := range triggers {
		if strings.Contains(text, t) {
			return true
		}
	}
	return false
}

// containsAnyWord checks if text contains any of the given words/phrases.
func containsAnyWord(text string, words []string) bool {
	for _, w := range words {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}
