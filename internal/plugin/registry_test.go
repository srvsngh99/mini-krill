package plugin

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/srvsngh99/mini-krill/internal/core"
)

// ---------------------------------------------------------------------------
// Mock skill for registry tests
// ---------------------------------------------------------------------------

type mockSkill struct {
	name string
	desc string
}

func (s *mockSkill) Name() string        { return s.name }
func (s *mockSkill) Description() string { return s.desc }
func (s *mockSkill) Execute(_ context.Context, input string, _ core.LLMProvider) (string, error) {
	return "mock: " + input, nil
}

// ---------------------------------------------------------------------------
// SkillRegistry tests
// ---------------------------------------------------------------------------

func TestSkillRegistryRegisterAndGet(t *testing.T) {
	reg := NewRegistry()

	skill := &mockSkill{name: "test-skill", desc: "a test skill"}
	if err := reg.Register(skill); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	got, ok := reg.Get("test-skill")
	if !ok {
		t.Fatal("Get() returned false, want true")
	}
	if got.Name() != "test-skill" {
		t.Errorf("Get().Name() = %q, want %q", got.Name(), "test-skill")
	}
}

func TestSkillRegistryGetNotFound(t *testing.T) {
	reg := NewRegistry()

	_, ok := reg.Get("nonexistent")
	if ok {
		t.Error("Get(nonexistent) returned true, want false")
	}
}

func TestSkillRegistryList(t *testing.T) {
	reg := NewRegistry()

	_ = reg.Register(&mockSkill{name: "alpha", desc: "first"})
	_ = reg.Register(&mockSkill{name: "beta", desc: "second"})
	_ = reg.Register(&mockSkill{name: "gamma", desc: "third"})

	infos := reg.List()
	if len(infos) != 3 {
		t.Fatalf("List() returned %d entries, want 3", len(infos))
	}

	// List should be sorted by name
	if infos[0].Name != "alpha" {
		t.Errorf("List()[0].Name = %q, want %q", infos[0].Name, "alpha")
	}
	if infos[1].Name != "beta" {
		t.Errorf("List()[1].Name = %q, want %q", infos[1].Name, "beta")
	}
	if infos[2].Name != "gamma" {
		t.Errorf("List()[2].Name = %q, want %q", infos[2].Name, "gamma")
	}

	// All should be enabled
	for _, info := range infos {
		if !info.Enabled {
			t.Errorf("List() skill %q is not enabled", info.Name)
		}
	}
}

func TestSkillRegistryUnregister(t *testing.T) {
	reg := NewRegistry()

	_ = reg.Register(&mockSkill{name: "removeme", desc: "to be removed"})

	if err := reg.Unregister("removeme"); err != nil {
		t.Fatalf("Unregister() error: %v", err)
	}

	_, ok := reg.Get("removeme")
	if ok {
		t.Error("Get() after Unregister() returned true, want false")
	}

	// Unregister non-existent should error
	if err := reg.Unregister("removeme"); err == nil {
		t.Error("Unregister(nonexistent) error = nil, want error")
	}
}

func TestDuplicateRegister(t *testing.T) {
	reg := NewRegistry()

	skill := &mockSkill{name: "dup", desc: "duplicate test"}
	if err := reg.Register(skill); err != nil {
		t.Fatalf("first Register() error: %v", err)
	}

	err := reg.Register(skill)
	if err == nil {
		t.Error("second Register() with same name returned nil error, want error")
	}
	if err != nil && !strings.Contains(err.Error(), "already registered") {
		t.Errorf("error = %q, want to contain 'already registered'", err.Error())
	}
}

func TestRegisterNilSkill(t *testing.T) {
	reg := NewRegistry()

	err := reg.Register(nil)
	if err == nil {
		t.Error("Register(nil) returned nil error, want error")
	}
}

// ---------------------------------------------------------------------------
// Built-in skills tests
// ---------------------------------------------------------------------------

func TestSkillRegistryBuiltins(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterBuiltins()

	infos := reg.List()
	if len(infos) != 3 {
		t.Fatalf("RegisterBuiltins() resulted in %d skills, want 3", len(infos))
	}

	// Verify the three expected built-in skills exist
	expected := map[string]bool{
		"recall":  false,
		"sysinfo": false,
		"time":    false,
	}

	for _, info := range infos {
		if _, ok := expected[info.Name]; ok {
			expected[info.Name] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("built-in skill %q not found in registry", name)
		}
	}
}

func TestSysInfoSkill(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterBuiltins()

	skill, ok := reg.Get("sysinfo")
	if !ok {
		t.Fatal("sysinfo skill not found")
	}

	output, err := skill.Execute(context.Background(), "", nil)
	if err != nil {
		t.Fatalf("sysinfo Execute() error: %v", err)
	}

	if output == "" {
		t.Fatal("sysinfo Execute() returned empty string")
	}

	// Verify it contains OS information
	if !strings.Contains(output, runtime.GOOS) {
		t.Errorf("sysinfo output does not contain OS %q", runtime.GOOS)
	}
	if !strings.Contains(output, runtime.GOARCH) {
		t.Errorf("sysinfo output does not contain arch %q", runtime.GOARCH)
	}
	if !strings.Contains(output, "System Information") {
		t.Error("sysinfo output does not contain 'System Information' header")
	}
}

func TestTimeSkill(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterBuiltins()

	skill, ok := reg.Get("time")
	if !ok {
		t.Fatal("time skill not found")
	}

	output, err := skill.Execute(context.Background(), "", nil)
	if err != nil {
		t.Fatalf("time Execute() error: %v", err)
	}

	if output == "" {
		t.Fatal("time Execute() returned empty string")
	}

	// Verify it contains date-like content (year)
	if !strings.Contains(output, "202") {
		t.Error("time output does not contain a recent year")
	}
	if !strings.Contains(output, "Current time") {
		t.Error("time output does not contain 'Current time' label")
	}
	if !strings.Contains(output, "Unix timestamp") {
		t.Error("time output does not contain 'Unix timestamp' label")
	}
}

func TestRecallSkill(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterBuiltins()

	skill, ok := reg.Get("recall")
	if !ok {
		t.Fatal("recall skill not found")
	}

	// Recall is a pass-through marker skill
	output, err := skill.Execute(context.Background(), "search query", nil)
	if err != nil {
		t.Fatalf("recall Execute() error: %v", err)
	}
	if output != "search query" {
		t.Errorf("recall Execute() = %q, want %q (pass-through)", output, "search query")
	}
}
