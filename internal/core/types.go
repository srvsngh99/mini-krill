// Package core defines all shared interfaces and types for Mini Krill.
// Every internal package depends only on core, config, and log - never on each other.
// Concrete wiring happens in cmd/minikrill/main.go via dependency injection.
package core

import (
	"context"
	"errors"
	"time"
)

// Version is set at build time via -ldflags
var Version = "0.1.4"

// ReservedAffinityPrefix namespaces per-task-type plan-affinity records in the
// memory store. Only the affinity store itself (Source == AffinitySource) may
// write keys under this prefix; any other writer is refused with
// ErrReservedKey so a stray `/remember affinity:cluster:... = junk` cannot
// corrupt a learned record.
const ReservedAffinityPrefix = "affinity:cluster:"

// AffinitySource is the MemoryEntry.Source the affinity store stamps on its
// own writes. It is the sole exemption from the ReservedAffinityPrefix guard.
const AffinitySource = "affinity-store"

// ErrReservedKey is returned by Memory.Store when a caller other than the
// owning subsystem tries to write into a reserved key namespace.
var ErrReservedKey = errors.New("memory: key uses a reserved namespace")

// ---------------------------------------------------------------------------
// LLM types
// ---------------------------------------------------------------------------

// Message represents a single chat message in a conversation.
type Message struct {
	Role    string `json:"role"` // system, user, assistant
	Content string `json:"content"`
}

// ChatOptions holds optional parameters for LLM calls.
type ChatOptions struct {
	Temperature  float64
	MaxTokens    int
	Model        string
	SystemPrompt string
}

// ChatOption is a functional option for ChatOptions.
type ChatOption func(*ChatOptions)

func WithTemperature(t float64) ChatOption { return func(o *ChatOptions) { o.Temperature = t } }
func WithMaxTokens(n int) ChatOption       { return func(o *ChatOptions) { o.MaxTokens = n } }
func WithModel(m string) ChatOption        { return func(o *ChatOptions) { o.Model = m } }
func WithSystemPrompt(s string) ChatOption { return func(o *ChatOptions) { o.SystemPrompt = s } }

// ApplyOptions merges functional options into a ChatOptions with defaults.
func ApplyOptions(opts []ChatOption) ChatOptions {
	o := ChatOptions{Temperature: 0.7, MaxTokens: 2048}
	for _, fn := range opts {
		fn(&o)
	}
	return o
}

// Response holds the result of an LLM call.
type Response struct {
	Content          string `json:"content"`
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
}

// StreamChunk is one piece of a streaming LLM response.
type StreamChunk struct {
	Content string
	Done    bool
	Err     error
}

// LLMProvider is the interface every LLM backend must implement.
type LLMProvider interface {
	Chat(ctx context.Context, messages []Message, opts ...ChatOption) (*Response, error)
	Stream(ctx context.Context, messages []Message, opts ...ChatOption) (<-chan StreamChunk, error)
	Name() string
	ModelName() string
	Available(ctx context.Context) bool
}

// ---------------------------------------------------------------------------
// Brain types
// ---------------------------------------------------------------------------

// MemoryEntry is a single item in the krill's memory.
type MemoryEntry struct {
	Key         string    `json:"key"`
	Value       string    `json:"value"`
	Tags        []string  `json:"tags,omitempty"`
	Scope       string    `json:"scope,omitempty"`  // "user", "project", "group", "task-outcome", "system"
	Source      string    `json:"source,omitempty"` // "user-explicit", "auto-learned", "task-outcome", "reflection", "feedback"
	AccessCount int       `json:"access_count,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	AccessedAt  time.Time `json:"accessed_at"`
}

// Personality defines the krill's character traits.
type Personality struct {
	Name       string   `json:"name" yaml:"name"`
	Traits     []string `json:"traits" yaml:"traits"`
	Style      string   `json:"style" yaml:"style"`
	Quirks     []string `json:"quirks" yaml:"quirks"`
	KrillFacts []string `json:"krill_facts" yaml:"krill_facts"`
	Greeting   string   `json:"greeting" yaml:"greeting"`
}

// Soul defines the krill's core identity and values.
type Soul struct {
	SystemPrompt string   `json:"system_prompt" yaml:"system_prompt"`
	Values       []string `json:"values" yaml:"values"`
	Boundaries   []string `json:"boundaries" yaml:"boundaries"`
	Identity     string   `json:"identity" yaml:"identity"`
}

// HealthStatus is a snapshot of system health, emitted by the heartbeat.
type HealthStatus struct {
	Alive      bool          `json:"alive"`
	Uptime     time.Duration `json:"uptime"`
	MemoryUsed uint64        `json:"memory_used"`
	LLMStatus  string        `json:"llm_status"`
	BrainOK    bool          `json:"brain_ok"`
	LastBeat   time.Time     `json:"last_beat"`
	Version    string        `json:"version"`
}

// Memory is the persistent memory store interface.
type Memory interface {
	Store(ctx context.Context, entry MemoryEntry) error
	Recall(ctx context.Context, key string) (*MemoryEntry, error)
	Search(ctx context.Context, query string, limit int) ([]MemoryEntry, error)
	RankedSearch(ctx context.Context, query string, scope string, limit int) ([]MemoryEntry, error)
	Forget(ctx context.Context, key string) error
	List(ctx context.Context) ([]MemoryEntry, error)
	Count() int
}

// ConversationStore persists conversation turns across sessions.
type ConversationStore interface {
	SaveTurn(channel, role, content string) error
	LoadRecent(channel string, n int) ([]Message, error)
	Search(channel, query string, since, until time.Time, limit int) ([]Message, error)
	// LastActivity reports the timestamp of the most recent stored turn for
	// channel (zero time if none). Persisted, so a session-boundary gap can
	// be measured across process restarts rather than from in-memory state.
	LastActivity(channel string) (time.Time, error)
	Close() error
}

// Brain orchestrates memory, personality, and soul.
type Brain interface {
	Memory() Memory
	ConversationStore() ConversationStore
	GetPersonality() *Personality
	GetSoul() *Soul
	SystemPrompt() string
	EnrichMessages(messages []Message) []Message
	RandomFact() string
}

// Heartbeat monitors system health and emits periodic status.
type Heartbeat interface {
	Start(ctx context.Context) error
	Stop() error
	Status() HealthStatus
	OnBeat(fn func(HealthStatus))
}

// ---------------------------------------------------------------------------
// Plugin types
// ---------------------------------------------------------------------------

// Skill is a pluggable capability the agent can invoke.
type Skill interface {
	Name() string
	Description() string
	Execute(ctx context.Context, input string, llm LLMProvider) (string, error)
}

// SkillInfo is metadata about a registered skill.
type SkillInfo struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	Enabled     bool   `json:"enabled" yaml:"enabled"`
	Category    string `json:"category" yaml:"category"` // "Built-in", "Self-Awareness", "Custom / YAML"
}

// SkillRegistry manages available skills.
type SkillRegistry interface {
	Register(skill Skill) error
	Unregister(name string) error
	// Get retrieves a skill by name. Returns nil, false both when the skill
	// is not registered AND when it is registered but disabled. Callers that
	// need to distinguish these cases should check IsEnabled separately.
	Get(name string) (Skill, bool)
	List() []SkillInfo
	SetEnabled(name string, enabled bool) error
	IsEnabled(name string) bool
}

// MCPTool describes a tool exposed by an MCP server.
type MCPTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema,omitempty"`
}

// MCPServerInfo is metadata about a registered MCP server.
type MCPServerInfo struct {
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	ToolCount int    `json:"tool_count"`
	Enabled   bool   `json:"enabled"`
}

// MCPServer is the interface for an MCP server connection.
type MCPServer interface {
	Name() string
	Connect(ctx context.Context) error
	Tools() []MCPTool
	CallTool(ctx context.Context, name string, args map[string]interface{}) (string, error)
	Close() error
}

// MCPRegistry manages MCP server connections.
type MCPRegistry interface {
	Register(name string, server MCPServer) error
	Get(name string) (MCPServer, bool)
	List() []MCPServerInfo
	AllTools() []MCPTool
	Close() error
	SetEnabled(name string, enabled bool) error
	IsEnabled(name string) bool
}

// ---------------------------------------------------------------------------
// Agent types
// ---------------------------------------------------------------------------

// PlanStep is one step in an execution plan.
type PlanStep struct {
	ID            int    `json:"id"`
	Description   string `json:"description"`
	Status        string `json:"status"` // pending, running, done, failed, skipped
	Output        string `json:"output,omitempty"`
	ToolHint      string `json:"tool_hint,omitempty"`      // suggested tool from planner
	NeedsApproval bool   `json:"needs_approval,omitempty"` // destructive step flag
}

// Plan is a set of steps the agent proposes before executing a task.
type Plan struct {
	Task     string     `json:"task"`
	Steps    []PlanStep `json:"steps"`
	Approved bool       `json:"approved"`
	Summary  string     `json:"summary"`
}

// SubKrill is a lightweight sub-agent spawned for a focused task.
type SubKrill struct {
	ID     string `json:"id"`
	Task   string `json:"task"`
	Status string `json:"status"` // spawned, running, done, failed
	Output string `json:"output,omitempty"`
}

// Agent is the main krill agent interface.
type Agent interface {
	Chat(ctx context.Context, input string) (string, error)
	Plan(ctx context.Context, task string) (*Plan, error)
	ExecutePlan(ctx context.Context, plan *Plan) (string, error)
	SpawnKrill(ctx context.Context, task string) (*SubKrill, error)
}

// PlatformAwareAgent extends Agent with a platform-aware entry point.
// Implementations process platform/chatID atomically with the message so
// background-task routing has the right destination.
type PlatformAwareAgent interface {
	Agent
	ChatFromPlatform(ctx context.Context, platform, chatID, input string) (string, error)
}

// TaskInfo is a serialisable snapshot of a durable task.
type TaskInfo struct {
	ID        string    `json:"id"`
	Task      string    `json:"task"`
	Status    string    `json:"status"` // queued, running, done, failed, cancelled
	Result    string    `json:"result,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TaskManager tracks long-running background tasks.
// Kept separate from Agent so the existing Agent contract stays unchanged.
type TaskManager interface {
	SubmitTask(ctx context.Context, userID, platform, chatID, task string) (taskID string, err error)
	ListTasks(userID string) []TaskInfo
	GetTask(id string) (*TaskInfo, bool)
	CancelTask(id string) error
}

// ---------------------------------------------------------------------------
// Chat types
// ---------------------------------------------------------------------------

// ChatMessage is a platform-agnostic incoming message.
type ChatMessage struct {
	Platform string `json:"platform"` // telegram, discord, tui, cli
	ChatID   string `json:"chat_id"`
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Text     string `json:"text"`
}

// ChatHandler processes incoming messages and returns responses.
type ChatHandler interface {
	HandleMessage(ctx context.Context, msg ChatMessage) (string, error)
}

// ChatBot is a messaging platform integration.
type ChatBot interface {
	Start(ctx context.Context) error
	Stop() error
	Platform() string
}

// ---------------------------------------------------------------------------
// Provider control types (runtime model/provider switching)
// ---------------------------------------------------------------------------

// ProviderInfo describes a single available LLM provider.
type ProviderInfo struct {
	Name     string   `json:"name"`
	Models   []string `json:"models"`
	IsActive bool     `json:"is_active"`
	NeedsKey bool     `json:"needs_key"`
	HasKey   bool     `json:"has_key"`
}

// ActiveInfo describes the currently active provider and model.
type ActiveInfo struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// ProviderControl is the interface for runtime provider/model management.
// Implemented by llm.ProviderManager, consumed by chat handlers.
type ProviderControl interface {
	ActiveInfo() ActiveInfo
	Switch(provider, model string) error
	ListProviders() []ProviderInfo
	ResolveTarget(input string) (provider, model string, ok bool)
}

// ---------------------------------------------------------------------------
// Doctor types
// ---------------------------------------------------------------------------

// CheckResult is the outcome of a single health check.
type CheckResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // ok, warn, fail
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// Doctor runs diagnostic health checks.
type Doctor interface {
	RunAll(ctx context.Context) []CheckResult
	RunCheck(ctx context.Context, name string) (*CheckResult, error)
	ListChecks() []string
}

// ---------------------------------------------------------------------------
// Krill facts - hidden throughout the codebase for delight
// ---------------------------------------------------------------------------

// KrillFacts are real facts about krill, the tiny crustaceans that power ocean ecosystems.
// Sprinkled throughout the UI for personality.
var KrillFacts = []string{
	"Antarctic krill (Euphausia superba) can live up to 11 years in the frozen deep",
	"A single swarm of krill can contain over 2 million tons of biomass",
	"Krill are bioluminescent - they glow blue-green in the dark ocean",
	"A krill's heart beats about 140 times per minute",
	"Krill form the largest animal aggregations on Earth",
	"Krill migrate vertically up to 600 meters every day - the largest daily migration on the planet",
	"Krill can swim backwards by flipping their tail - it is called lobstering",
	"Despite being tiny (6cm), krill are keystone species - remove them and ecosystems collapse",
	"Krill have been around for over 130 million years - they survived the dinosaur extinction",
	"A blue whale can eat up to 4 tons of krill per day",
	"Krill molt their exoskeleton up to 10 times per year as they grow",
	"Krill eggs sink up to 3000 meters before hatching and swimming back up",
	"Krill produce omega-3 fatty acids that power entire marine food chains",
	"In total biomass, krill outweigh all humans on Earth",
	"Krill communicate through bioluminescent flashes in the deep ocean",
	"Baby krill go through 12 developmental stages before becoming adults",
	"Krill swarms can be so dense they are visible from space",
	"The word 'krill' comes from Norwegian, meaning 'small fry of fish'",
	"Krill have the largest biomass of any multi-cellular animal species on Earth",
	"Some krill species can survive without food for up to 200 days by shrinking their own body",
}
