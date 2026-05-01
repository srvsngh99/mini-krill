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
	if len(infos) < 3 {
		t.Fatalf("RegisterBuiltins() resulted in %d skills, want at least 3", len(infos))
	}

	// Verify the expected built-in skills exist
	expected := map[string]bool{
		"recall":  false,
		"sysinfo": false,
		"time":    false,
		"search":  false,
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

// ---------------------------------------------------------------------------
// Enable/disable tests
// ---------------------------------------------------------------------------

func TestSkillRegistryDisableAndGet(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(&mockSkill{name: "search", desc: "web search"})

	// Disable
	if err := reg.SetEnabled("search", false); err != nil {
		t.Fatalf("SetEnabled(false) error: %v", err)
	}

	// Get should return false for disabled skill
	_, ok := reg.Get("search")
	if ok {
		t.Error("Get() returned true for disabled skill, want false")
	}

	// List should show Enabled: false
	for _, info := range reg.List() {
		if info.Name == "search" && info.Enabled {
			t.Error("List() shows disabled skill as enabled")
		}
	}
}

func TestSkillRegistryReEnable(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(&mockSkill{name: "time", desc: "time skill"})

	_ = reg.SetEnabled("time", false)
	_, ok := reg.Get("time")
	if ok {
		t.Fatal("Get() should return false when disabled")
	}

	_ = reg.SetEnabled("time", true)
	_, ok = reg.Get("time")
	if !ok {
		t.Error("Get() should return true after re-enabling")
	}

	if !reg.IsEnabled("time") {
		t.Error("IsEnabled() should return true after re-enabling")
	}
}

func TestSkillRegistrySelfSkillUndisableable(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(&mockSkill{name: "self:inspect", desc: "inspect"})

	err := reg.SetEnabled("self:inspect", false)
	if err == nil {
		t.Error("SetEnabled(false) on self-skill should return error")
	}

	// Should still be gettable
	_, ok := reg.Get("self:inspect")
	if !ok {
		t.Error("self-skill should remain gettable after failed disable")
	}

	if !reg.IsEnabled("self:inspect") {
		t.Error("self-skill should remain enabled after failed disable")
	}
}

func TestSkillRegistryDisableUnknown(t *testing.T) {
	reg := NewRegistry()

	err := reg.SetEnabled("nonexistent", false)
	if err == nil {
		t.Error("SetEnabled on nonexistent skill should return error")
	}
}

func TestFeatureSkillsRegistration(t *testing.T) {
	reg := NewRegistry()

	// Register with nil reminders and no Telegram — should get youtube, research, web
	reg.RegisterFeatureSkills(FeatureContext{})

	expected := []string{"youtube", "research", "web"}
	for _, name := range expected {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("feature skill %q not registered", name)
		}
	}

	// remind and notify should NOT be registered (nil store, no token)
	if _, ok := reg.Get("remind"); ok {
		t.Error("remind should not be registered without a Store")
	}
	if _, ok := reg.Get("notify"); ok {
		t.Error("notify should not be registered without Telegram config")
	}

	// Verify categories are set to Feature
	for _, info := range reg.List() {
		for _, name := range expected {
			if info.Name == name && info.Category != "Feature" {
				t.Errorf("feature skill %q category = %q, want Feature", name, info.Category)
			}
		}
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
