// Package doctor provides diagnostic health checks for Mini Krill.
// Think of it as the krill's immune system - constantly monitoring internal
// health and flagging problems before they become critical.
// Krill fact: krill can survive without food for up to 200 days by shrinking
// their own body. The doctor ensures Mini Krill never has to resort to that.
package doctor

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// ---------------------------------------------------------------------------
// Health check thresholds - the krill's vital sign boundaries
// ---------------------------------------------------------------------------

const (
	// Disk space thresholds in bytes
	diskFailThreshold = 50 * 1024 * 1024  // <50 MB = fail
	diskWarnThreshold = 100 * 1024 * 1024 // <100 MB = warn

	// Memory threshold in bytes
	memoryWarnThreshold = 500 * 1024 * 1024 // >500 MB allocated = warn

	// HTTP timeout for Ollama health check
	ollamaTimeout = 5 * time.Second
)

// Status constants for check results
const (
	StatusOK   = "ok"
	StatusWarn = "warn"
	StatusFail = "fail"
)

// ---------------------------------------------------------------------------
// Check function type - each health check is a simple function
// ---------------------------------------------------------------------------

// checkFunc is the signature for a single health check.
type checkFunc func(ctx context.Context) core.CheckResult

// ---------------------------------------------------------------------------
// KrillDoctor - the diagnostic engine
// ---------------------------------------------------------------------------

// KrillDoctor implements core.Doctor, running diagnostic health checks
// across all Mini Krill subsystems. Like a marine biologist monitoring
// krill health in a research aquarium.
type KrillDoctor struct {
	ollamaHost string
	llm        core.LLMProvider
	brainDir   string
	checks     map[string]checkFunc
	checkOrder []string // deterministic ordering for output
}

// NewDoctor creates a new KrillDoctor with the standard suite of health checks.
// The doctor adapts to what it is given - nil LLM provider just means the LLM
// check will report unavailable, not crash.
func NewDoctor(ollamaHost string, llm core.LLMProvider, brainDir string) *KrillDoctor {
	d := &KrillDoctor{
		ollamaHost: ollamaHost,
		llm:        llm,
		brainDir:   brainDir,
		checks:     make(map[string]checkFunc),
	}

	// Register all health checks in a defined order.
	// Like a krill's 12 developmental stages - each check builds on the previous.
	orderedChecks := []struct {
		name string
		fn   checkFunc
	}{
		{"ollama", d.checkOllama},
		{"llm", d.checkLLM},
		{"brain", d.checkBrain},
		{"disk", d.checkDisk},
		{"memory", d.checkMemory},
		{"config", d.checkConfig},
	}

	for _, c := range orderedChecks {
		d.checks[c.name] = c.fn
		d.checkOrder = append(d.checkOrder, c.name)
	}

	return d
}

// RunAll executes all registered health checks and returns the results.
// Checks run sequentially to avoid overwhelming the system - krill move
// as one coordinated swarm.
func (d *KrillDoctor) RunAll(ctx context.Context) []core.CheckResult {
	log.Debug("running all health checks", "count", len(d.checkOrder))

	results := make([]core.CheckResult, 0, len(d.checkOrder))
	for _, name := range d.checkOrder {
		fn := d.checks[name]
		result := fn(ctx)
		results = append(results, result)
	}

	return results
}

// RunCheck executes a single named health check.
func (d *KrillDoctor) RunCheck(ctx context.Context, name string) (*core.CheckResult, error) {
	fn, ok := d.checks[name]
	if !ok {
		return nil, fmt.Errorf("unknown health check: %q", name)
	}

	result := fn(ctx)
	return &result, nil
}

// ListChecks returns the names of all available health checks in order.
func (d *KrillDoctor) ListChecks() []string {
	result := make([]string, len(d.checkOrder))
	copy(result, d.checkOrder)
	return result
}

// ---------------------------------------------------------------------------
// Individual health checks - each one monitors a vital organ
// ---------------------------------------------------------------------------

// checkOllama verifies the Ollama API is reachable by hitting /api/tags.
// Like checking if the ocean current is flowing - no current, no food for krill.
func (d *KrillDoctor) checkOllama(ctx context.Context) core.CheckResult {
	if d.ollamaHost == "" {
		return core.CheckResult{
			Name:    "ollama",
			Status:  StatusWarn,
			Message: "no Ollama host configured",
		}
	}

	url := strings.TrimRight(d.ollamaHost, "/") + "/api/tags"

	client := &http.Client{Timeout: ollamaTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return core.CheckResult{
			Name:    "ollama",
			Status:  StatusFail,
			Message: "failed to create request",
			Details: err.Error(),
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return core.CheckResult{
			Name:    "ollama",
			Status:  StatusFail,
			Message: "Ollama is not reachable",
			Details: err.Error(),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return core.CheckResult{
			Name:    "ollama",
			Status:  StatusFail,
			Message: fmt.Sprintf("Ollama returned HTTP %d", resp.StatusCode),
		}
	}

	return core.CheckResult{
		Name:    "ollama",
		Status:  StatusOK,
		Message: "Ollama is running and responsive",
	}
}

// checkLLM verifies the LLM provider is available by calling Available().
// The krill's neural ganglion - if the brain is offline, the krill cannot think.
func (d *KrillDoctor) checkLLM(ctx context.Context) core.CheckResult {
	if d.llm == nil {
		return core.CheckResult{
			Name:    "llm",
			Status:  StatusFail,
			Message: "no LLM provider configured",
		}
	}

	available := d.llm.Available(ctx)
	if !available {
		return core.CheckResult{
			Name:    "llm",
			Status:  StatusFail,
			Message: fmt.Sprintf("LLM provider %q is not available", d.llm.Name()),
			Details: fmt.Sprintf("provider=%s model=%s", d.llm.Name(), d.llm.ModelName()),
		}
	}

	return core.CheckResult{
		Name:    "llm",
		Status:  StatusOK,
		Message: fmt.Sprintf("LLM provider %q is available (model: %s)", d.llm.Name(), d.llm.ModelName()),
	}
}

// checkBrain verifies the brain data directory exists and is writable.
// The krill's memory center - without it, no learning, no personality.
func (d *KrillDoctor) checkBrain(ctx context.Context) core.CheckResult {
	if d.brainDir == "" {
		return core.CheckResult{
			Name:    "brain",
			Status:  StatusFail,
			Message: "no brain directory configured",
		}
	}

	// Check directory exists
	info, err := os.Stat(d.brainDir)
	if err != nil {
		if os.IsNotExist(err) {
			return core.CheckResult{
				Name:    "brain",
				Status:  StatusFail,
				Message: "brain directory does not exist",
				Details: d.brainDir,
			}
		}
		return core.CheckResult{
			Name:    "brain",
			Status:  StatusFail,
			Message: "cannot access brain directory",
			Details: err.Error(),
		}
	}

	if !info.IsDir() {
		return core.CheckResult{
			Name:    "brain",
			Status:  StatusFail,
			Message: "brain path is not a directory",
			Details: d.brainDir,
		}
	}

	// Check writable by creating and removing a temp file
	tmpFile := filepath.Join(d.brainDir, ".doctor-write-test")
	if err := os.WriteFile(tmpFile, []byte("krill"), 0644); err != nil {
		return core.CheckResult{
			Name:    "brain",
			Status:  StatusFail,
			Message: "brain directory is not writable",
			Details: err.Error(),
		}
	}
	os.Remove(tmpFile)

	return core.CheckResult{
		Name:    "brain",
		Status:  StatusOK,
		Message: "brain directory is accessible and writable",
		Details: d.brainDir,
	}
}

// checkDisk verifies there is sufficient free disk space on the volume
// containing the brain directory.
// Like monitoring ocean depth - krill need enough water column to migrate.
func (d *KrillDoctor) checkDisk(ctx context.Context) core.CheckResult {
	if d.brainDir == "" {
		return core.CheckResult{
			Name:    "disk",
			Status:  StatusWarn,
			Message: "no brain directory configured, cannot check disk space",
		}
	}

	// Check the directory exists first
	if _, err := os.Stat(d.brainDir); err != nil {
		return core.CheckResult{
			Name:    "disk",
			Status:  StatusWarn,
			Message: "brain directory does not exist, cannot check disk space",
			Details: d.brainDir,
		}
	}

	return d.checkDiskPlatform()
}

// checkMemory examines the Go runtime's memory usage.
// Krill are tiny but efficient - this check ensures the process is not bloating.
func (d *KrillDoctor) checkMemory(ctx context.Context) core.CheckResult {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	allocMB := float64(mem.Alloc) / (1024 * 1024)
	sysMB := float64(mem.Sys) / (1024 * 1024)

	details := fmt.Sprintf("alloc=%.1f MB, sys=%.1f MB, goroutines=%d, gc_cycles=%d",
		allocMB, sysMB, runtime.NumGoroutine(), mem.NumGC)

	if mem.Alloc > memoryWarnThreshold {
		return core.CheckResult{
			Name:    "memory",
			Status:  StatusWarn,
			Message: fmt.Sprintf("high memory usage: %.1f MB allocated", allocMB),
			Details: details,
		}
	}

	return core.CheckResult{
		Name:    "memory",
		Status:  StatusOK,
		Message: fmt.Sprintf("memory usage nominal: %.1f MB allocated", allocMB),
		Details: details,
	}
}

// checkConfig tries to load the configuration and verifies it parses correctly.
// The krill's DNA - if the config is broken, nothing else will work right.
func (d *KrillDoctor) checkConfig(ctx context.Context) core.CheckResult {
	_, err := config.Load()
	if err != nil {
		return core.CheckResult{
			Name:    "config",
			Status:  StatusFail,
			Message: "configuration failed to load",
			Details: err.Error(),
		}
	}

	return core.CheckResult{
		Name:    "config",
		Status:  StatusOK,
		Message: "configuration loaded successfully",
	}
}

// ---------------------------------------------------------------------------
// Result formatting - making health checks beautiful
// ---------------------------------------------------------------------------

// FormatResults pretty-prints a slice of CheckResults with status symbols
// and colors (via ANSI escape codes). The output is designed for terminal
// display - clear, concise, and easy to scan.
func FormatResults(results []core.CheckResult) string {
	if len(results) == 0 {
		return "No health checks were run."
	}

	// Sort by status severity: fail first, then warn, then ok
	sorted := make([]core.CheckResult, len(results))
	copy(sorted, results)
	sort.Slice(sorted, func(i, j int) bool {
		return statusPriority(sorted[i].Status) > statusPriority(sorted[j].Status)
	})

	var sb strings.Builder
	sb.WriteString("\n  Mini Krill Health Report\n")
	sb.WriteString("  " + strings.Repeat("-", 40) + "\n\n")

	for _, r := range sorted {
		symbol, color := statusSymbol(r.Status)
		// ANSI color: green=32, yellow=33, red=31
		sb.WriteString(fmt.Sprintf("  \033[%dm[%s]\033[0m %-8s - %s\n", color, symbol, r.Name, r.Message))
		if r.Details != "" {
			sb.WriteString(fmt.Sprintf("           %s\n", r.Details))
		}
	}

	// Summary line
	ok, warn, fail := countStatuses(sorted)
	sb.WriteString(fmt.Sprintf("\n  Summary: %d passed, %d warnings, %d failed\n", ok, warn, fail))

	return sb.String()
}

// statusSymbol returns the display symbol and ANSI color code for a status.
func statusSymbol(status string) (string, int) {
	switch status {
	case StatusOK:
		return "OK", 32 // green
	case StatusWarn:
		return "WARN", 33 // yellow
	case StatusFail:
		return "FAIL", 31 // red
	default:
		return "??", 37 // white
	}
}

// statusPriority returns a sort priority for status (higher = more severe).
func statusPriority(status string) int {
	switch status {
	case StatusFail:
		return 2
	case StatusWarn:
		return 1
	default:
		return 0
	}
}

// countStatuses tallies the number of ok, warn, and fail results.
func countStatuses(results []core.CheckResult) (ok, warn, fail int) {
	for _, r := range results {
		switch r.Status {
		case StatusOK:
			ok++
		case StatusWarn:
			warn++
		case StatusFail:
			fail++
		}
	}
	return
}
