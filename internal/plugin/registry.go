// Package plugin implements the skill registry and MCP server registry for Mini Krill.
package plugin

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"text/template"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Skill Registry
// ---------------------------------------------------------------------------

// SkillRegistryImpl is the concrete implementation of core.SkillRegistry.
// Thread-safe via sync.RWMutex.
type SkillRegistryImpl struct {
	mu     sync.RWMutex
	skills map[string]core.Skill
}

// NewRegistry creates a new, empty skill registry.
func NewRegistry() *SkillRegistryImpl {
	return &SkillRegistryImpl{
		skills: make(map[string]core.Skill),
	}
}

// Register adds a skill to the registry.
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
	log.Debug("skill registered", "name", name)
	return nil
}

// Unregister removes a skill from the registry by name.
func (r *SkillRegistryImpl) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.skills[name]; !exists {
		return fmt.Errorf("skill %q not found", name)
	}

	delete(r.skills, name)
	log.Debug("skill removed", "name", name)
	return nil
}

// Get retrieves a skill by name.
func (r *SkillRegistryImpl) Get(name string) (core.Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	skill, ok := r.skills[name]
	return skill, ok
}

// List returns metadata for all registered skills, sorted by name.
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

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})

	return infos
}

// ---------------------------------------------------------------------------
// YAML Skill loader
// ---------------------------------------------------------------------------

type yamlSkillDef struct {
	Name           string `yaml:"name"`
	Description    string `yaml:"description"`
	PromptTemplate string `yaml:"prompt_template"`
}

// YAMLSkill is a skill loaded from a YAML definition file.
type YAMLSkill struct {
	name        string
	description string
	tmpl        *template.Template
	rawTemplate string
}

func (s *YAMLSkill) Name() string        { return s.name }
func (s *YAMLSkill) Description() string { return s.description }

func (s *YAMLSkill) Execute(ctx context.Context, input string, llm core.LLMProvider) (string, error) {
	if llm == nil {
		return "", fmt.Errorf("no LLM provider available for skill %q", s.name)
	}

	data := struct{ Input string }{Input: input}
	var buf bytes.Buffer
	if err := s.tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render template for skill %q: %w", s.name, err)
	}

	resp, err := llm.Chat(ctx, []core.Message{
		{Role: "user", Content: buf.String()},
	})
	if err != nil {
		return "", fmt.Errorf("skill %q LLM call failed: %w", s.name, err)
	}

	return resp.Content, nil
}

// LoadSkillsFromDir reads all .yaml/.yml files from a directory and registers them.
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

	log.Info("loaded YAML skills", "dir", dir, "count", loaded)
	return nil
}

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
