// Package plugin implements the skill registry and MCP server registry for Mini Krill.
// Skills are pluggable capabilities the krill agent can invoke - from built-in system
// tools to user-defined YAML prompt templates.
// Krill fact: krill form the largest animal aggregations on Earth - this registry
// aggregates all the agent's capabilities into one swarm.
package plugin

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Skill Registry - the swarm coordinator for all agent capabilities
// ---------------------------------------------------------------------------

// SkillRegistryImpl is the concrete implementation of core.SkillRegistry.
// Thread-safe via sync.RWMutex, suitable for concurrent access from
// the agent loop, TUI, and sub-krills simultaneously.
type SkillRegistryImpl struct {
	mu     sync.RWMutex
	skills map[string]core.Skill
}

// NewRegistry creates a new, empty skill registry.
// Like a krill swarm starting with a single individual - it grows from here.
func NewRegistry() *SkillRegistryImpl {
	return &SkillRegistryImpl{
		skills: make(map[string]core.Skill),
	}
}

// Register adds a skill to the registry. Returns an error if a skill with the
// same name is already registered - no silent overwrites allowed.
func (r *SkillRegistryImpl) Register(skill core.Skill) error {
	if skill == nil {
		return fmt.Errorf("cannot register nil skill")
	}
	name := skill.Name()
	if name == "" {
		return fmt.Errorf("cannot register skill with empty name")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.skills[name]; exists {
		return fmt.Errorf("skill %q already registered", name)
	}

	r.skills[name] = skill
	log.Debug("skill registered in the swarm", "name", name)
	return nil
}

// Unregister removes a skill from the registry by name.
// Returns an error if the skill is not found.
func (r *SkillRegistryImpl) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.skills[name]; !exists {
		return fmt.Errorf("skill %q not found", name)
	}

	delete(r.skills, name)
	log.Debug("skill removed from the swarm", "name", name)
	return nil
}

// Get retrieves a skill by name. Returns the skill and true if found,
// or nil and false if not registered.
func (r *SkillRegistryImpl) Get(name string) (core.Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	skill, ok := r.skills[name]
	return skill, ok
}

// List returns metadata for all registered skills, sorted by name for
// deterministic output. Every skill is marked as enabled - if it is
// registered, it is ready to deploy.
func (r *SkillRegistryImpl) List() []core.SkillInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]core.SkillInfo, 0, len(r.skills))
	for _, s := range r.skills {
		infos = append(infos, core.SkillInfo{
			Name:        s.Name(),
			Description: s.Description(),
			Enabled:     true,
		})
	}

	// Sort by name so output is stable - krill swarms have order in their chaos
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})

	return infos
}

// ---------------------------------------------------------------------------
// YAML Skill loader - user-defined prompt templates
// ---------------------------------------------------------------------------

// yamlSkillDef is the on-disk YAML format for a skill definition.
type yamlSkillDef struct {
	Name           string `yaml:"name"`
	Description    string `yaml:"description"`
	PromptTemplate string `yaml:"prompt_template"`
}

// YAMLSkill is a skill loaded from a YAML definition file. Its Execute method
// renders the prompt_template with the user's input and sends it to the LLM.
// Like krill adapting to different ocean currents - each YAML skill adapts
// the LLM to a different task.
type YAMLSkill struct {
	name        string
	description string
	tmpl        *template.Template
	rawTemplate string
}

// Name returns the skill's identifier.
func (s *YAMLSkill) Name() string { return s.name }

// Description returns a human-readable description of what the skill does.
func (s *YAMLSkill) Description() string { return s.description }

// Execute renders the prompt template with the input and sends it to the LLM.
// The template receives a struct with an .Input field for use in {{.Input}}.
func (s *YAMLSkill) Execute(ctx context.Context, input string, llm core.LLMProvider) (string, error) {
	if llm == nil {
		return "", fmt.Errorf("no LLM provider available for skill %q", s.name)
	}

	// Render the prompt template with the user's input
	data := struct{ Input string }{Input: input}
	var buf bytes.Buffer
	if err := s.tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render template for skill %q: %w", s.name, err)
	}

	prompt := buf.String()
	log.Debug("YAML skill executing", "skill", s.name, "prompt_len", len(prompt))

	// Send to LLM as a user message
	messages := []core.Message{
		{Role: "user", Content: prompt},
	}

	resp, err := llm.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("skill %q LLM call failed: %w", s.name, err)
	}

	return resp.Content, nil
}

// LoadSkillsFromDir reads all .yaml and .yml files from the given directory,
// parses them as skill definitions, and registers them in the registry.
// Skips malformed files with a warning rather than failing the whole load -
// one bad egg should not spoil the swarm.
func (r *SkillRegistryImpl) LoadSkillsFromDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Debug("skills directory does not exist, skipping", "dir", dir)
			return nil
		}
		return fmt.Errorf("read skills directory %q: %w", dir, err)
	}

	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		skill, err := loadYAMLSkill(path)
		if err != nil {
			log.Warn("skipping malformed skill file", "path", path, "error", err)
			continue
		}

		if err := r.Register(skill); err != nil {
			log.Warn("could not register skill from file", "path", path, "error", err)
			continue
		}

		loaded++
	}

	log.Info("loaded YAML skills from directory", "dir", dir, "count", loaded)
	return nil
}

// loadYAMLSkill parses a single YAML skill definition file.
func loadYAMLSkill(path string) (*YAMLSkill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	var def yamlSkillDef
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}

	if def.Name == "" {
		return nil, fmt.Errorf("skill definition missing 'name' field")
	}
	if def.PromptTemplate == "" {
		return nil, fmt.Errorf("skill %q missing 'prompt_template' field", def.Name)
	}

	tmpl, err := template.New(def.Name).Parse(def.PromptTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse template for skill %q: %w", def.Name, err)
	}

	return &YAMLSkill{
		name:        def.Name,
		description: def.Description,
		tmpl:        tmpl,
		rawTemplate: def.PromptTemplate,
	}, nil
}

// ---------------------------------------------------------------------------
// Built-in skills - the krill's innate abilities
// ---------------------------------------------------------------------------

// RegisterBuiltins registers the default set of built-in skills that ship
// with every Mini Krill instance. These are the krill's innate survival
// instincts - always available, no YAML required.
func (r *SkillRegistryImpl) RegisterBuiltins() {
	builtins := []core.Skill{
		&recallSkill{},
		&sysinfoSkill{},
		&timeSkill{},
	}

	for _, s := range builtins {
		if err := r.Register(s); err != nil {
			// Built-in registration should never fail unless called twice.
			// Log it but do not crash - krill are resilient survivors.
			log.Warn("failed to register built-in skill", "name", s.Name(), "error", err)
		}
	}

	log.Debug("built-in skills registered", "count", len(builtins))
}

// --- recall skill ---
// A marker skill that signals the agent to search its memory.
// The actual memory lookup is handled by the agent layer - this skill
// just returns the input so the agent knows what to search for.

type recallSkill struct{}

func (s *recallSkill) Name() string        { return "recall" }
func (s *recallSkill) Description() string { return "Search the krill's memory for information" }

// Execute returns the input as-is. The recall skill is a marker - the agent
// intercepts it and performs the actual memory lookup. Like krill using
// bioluminescent signals to communicate intent to the swarm.
func (s *recallSkill) Execute(_ context.Context, input string, _ core.LLMProvider) (string, error) {
	return input, nil
}

// --- sysinfo skill ---
// Reports system information: OS, architecture, CPU count, and memory stats.
// The krill always knows its environment - survival depends on awareness.

type sysinfoSkill struct{}

func (s *sysinfoSkill) Name() string        { return "sysinfo" }
func (s *sysinfoSkill) Description() string { return "Get system information" }

// Execute gathers and returns system information. No LLM needed - this is
// pure introspection, like a krill sensing water temperature and pressure.
func (s *sysinfoSkill) Execute(_ context.Context, _ string, _ core.LLMProvider) (string, error) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	// Format memory in human-readable units
	totalAlloc := formatBytes(mem.TotalAlloc)
	sysMemory := formatBytes(mem.Sys)
	heapInUse := formatBytes(mem.HeapInuse)

	info := fmt.Sprintf(
		"System Information:\n"+
			"  OS:           %s\n"+
			"  Architecture: %s\n"+
			"  CPUs:         %d\n"+
			"  Go version:   %s\n"+
			"  Goroutines:   %d\n"+
			"  Memory:\n"+
			"    Allocated:  %s\n"+
			"    System:     %s\n"+
			"    Heap in use: %s\n"+
			"    GC cycles:  %d",
		runtime.GOOS,
		runtime.GOARCH,
		runtime.NumCPU(),
		runtime.Version(),
		runtime.NumGoroutine(),
		totalAlloc,
		sysMemory,
		heapInUse,
		mem.NumGC,
	)

	return info, nil
}

// --- time skill ---
// Returns the current date and time. Even deep-sea krill track the daily
// cycle - they migrate vertically based on time of day.

type timeSkill struct{}

func (s *timeSkill) Name() string        { return "time" }
func (s *timeSkill) Description() string { return "Get current date and time" }

// Execute returns the current date and time in a human-readable format.
// Krill navigate by circadian rhythm - this skill gives the agent its clock.
func (s *timeSkill) Execute(_ context.Context, _ string, _ core.LLMProvider) (string, error) {
	now := time.Now()
	return fmt.Sprintf(
		"Current time: %s\nTimezone: %s\nUnix timestamp: %d",
		now.Format("2006-01-02 15:04:05 MST"),
		now.Location().String(),
		now.Unix(),
	), nil
}

// ---------------------------------------------------------------------------
// Utility functions
// ---------------------------------------------------------------------------

// formatBytes converts bytes to a human-readable string.
func formatBytes(b uint64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
