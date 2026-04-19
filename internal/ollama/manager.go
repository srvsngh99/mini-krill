// Package ollama manages the lifecycle of a local Ollama installation:
// detecting, installing, starting, stopping, and pulling models.
package ollama

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/srvsngh99/mini-krill/internal/config"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// ModelInfo describes an Ollama model present on the local machine.
type ModelInfo struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
}

// OllamaManager handles lifecycle operations for a local Ollama instance.
type OllamaManager struct {
	host   string
	client *http.Client

	mu          sync.Mutex
	proc        *os.Process // tracked "ollama serve" process, if we started it
	weStartedIt bool        // true only if Start() spawned the process

	monitorCancel context.CancelFunc // stops the background health monitor
}

// NewManager creates an OllamaManager targeting the configured host.
func NewManager(cfg config.OllamaConfig) *OllamaManager {
	host := cfg.Host
	if host == "" {
		host = "http://localhost:11434"
	}
	host = strings.TrimRight(host, "/")

	return &OllamaManager{
		host: host,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// ---------------------------------------------------------------------------
// Detection
// ---------------------------------------------------------------------------

// IsInstalled checks whether the "ollama" binary is available on PATH.
func (m *OllamaManager) IsInstalled() bool {
	_, err := exec.LookPath("ollama")
	return err == nil
}

// ---------------------------------------------------------------------------
// Installation
// ---------------------------------------------------------------------------

// Install attempts a platform-aware installation of Ollama.
// - Linux: uses the official install script (curl | sh)
// - macOS: tries Homebrew first, falls back to the install script
// - Windows: prints guidance to install manually from ollama.com/download
func (m *OllamaManager) Install(ctx context.Context) error {
	if m.IsInstalled() {
		log.Info("ollama is already installed")
		return nil
	}

	log.Info("installing ollama", "platform", runtime.GOOS)

	switch runtime.GOOS {
	case "linux":
		return m.installViaScript(ctx)

	case "darwin":
		// Try Homebrew first
		if _, err := exec.LookPath("brew"); err == nil {
			log.Info("installing ollama via homebrew")
			cmd := exec.CommandContext(ctx, "brew", "install", "ollama")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				log.Warn("brew install failed, falling back to install script", "error", err)
				return m.installViaScript(ctx)
			}
			return nil
		}
		return m.installViaScript(ctx)

	case "windows":
		return fmt.Errorf("automatic Ollama installation is not supported on Windows - please download from https://ollama.com/download and install manually")

	default:
		return fmt.Errorf("unsupported platform for Ollama installation: %s", runtime.GOOS)
	}
}

// installViaScript runs the official Ollama install script.
func (m *OllamaManager) installViaScript(ctx context.Context) error {
	log.Info("installing ollama via install script")

	// Check that curl is available
	if _, err := exec.LookPath("curl"); err != nil {
		return fmt.Errorf("curl is required to install ollama but was not found on PATH")
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", "curl -fsSL https://ollama.com/install.sh | sh")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ollama install script failed: %w", err)
	}

	// Verify installation succeeded
	if !m.IsInstalled() {
		return fmt.Errorf("ollama install script completed but 'ollama' binary not found on PATH")
	}

	log.Info("ollama installed successfully")
	return nil
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// EnsureRunning checks if Ollama is healthy and starts it if not.
// Intended as the single entry point for "make sure Ollama is up".
func (m *OllamaManager) EnsureRunning(ctx context.Context) error {
	if m.isHealthy(ctx) {
		log.Debug("ollama is already running")
		m.mu.Lock()
		m.weStartedIt = false
		m.mu.Unlock()
		return nil
	}
	return m.Start(ctx)
}

// Start launches "ollama serve" as a background process. If Ollama is already
// running (health check passes), this is a no-op.
func (m *OllamaManager) Start(ctx context.Context) error {
	// Already running?
	if m.isHealthy(ctx) {
		log.Debug("ollama is already running")
		m.mu.Lock()
		m.weStartedIt = false
		m.mu.Unlock()
		return nil
	}

	if !m.IsInstalled() {
		return fmt.Errorf("ollama is not installed - run 'krill init' to set it up")
	}

	log.Info("starting ollama serve")

	cmd := exec.Command("ollama", "serve")
	// Detach from our stdin so it runs as a daemon
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ollama serve: %w", err)
	}

	m.mu.Lock()
	m.proc = cmd.Process
	m.weStartedIt = true
	m.mu.Unlock()

	// Release the process so it doesn't become a zombie if we exit
	go func() {
		_ = cmd.Wait()
	}()

	// Wait for Ollama to become healthy (up to 15 seconds)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if m.isHealthy(ctx) {
			log.Info("ollama is ready", "host", m.host)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	return fmt.Errorf("ollama started but did not become healthy within 15 seconds")
}

// Stop sends SIGTERM to the ollama process we started. If we did not start it
// (e.g., it was already running), this is a no-op.
func (m *OllamaManager) Stop() error {
	m.mu.Lock()
	proc := m.proc
	m.proc = nil
	m.mu.Unlock()

	if proc == nil {
		log.Debug("no ollama process to stop (we did not start it)")
		return nil
	}

	log.Info("stopping ollama", "pid", proc.Pid)
	if err := proc.Signal(os.Interrupt); err != nil {
		// Try harder with Kill
		log.Warn("interrupt failed, sending kill", "error", err)
		if err := proc.Kill(); err != nil {
			return fmt.Errorf("failed to kill ollama process: %w", err)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Model management
// ---------------------------------------------------------------------------

// Pull downloads a model from the Ollama registry. It reads the streaming
// progress response and logs status updates.
func (m *OllamaManager) Pull(ctx context.Context, model string) error {
	log.Info("pulling ollama model", "model", model)

	reqBody, err := json.Marshal(map[string]string{"name": model})
	if err != nil {
		return fmt.Errorf("marshal pull request: %w", err)
	}

	url := m.host + "/api/pull"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(reqBody)))
	if err != nil {
		return fmt.Errorf("create pull request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Use a client with no timeout since model downloads can take a long time
	pullClient := &http.Client{}
	resp, err := pullClient.Do(req)
	if err != nil {
		return fmt.Errorf("pull request failed (is ollama running?): %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pull returned status %d: %s", resp.StatusCode, string(errBody))
	}

	// Read streaming NDJSON progress
	scanner := bufio.NewScanner(resp.Body)
	// Allow large lines for download progress messages
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var lastStatus string
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		var progress struct {
			Status    string `json:"status"`
			Digest    string `json:"digest"`
			Total     int64  `json:"total"`
			Completed int64  `json:"completed"`
			Error     string `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &progress); err != nil {
			// Skip malformed lines gracefully
			continue
		}

		if progress.Error != "" {
			return fmt.Errorf("ollama pull error: %s", progress.Error)
		}

		// Log status changes (not every progress tick)
		if progress.Status != lastStatus {
			log.Info("pull progress", "model", model, "status", progress.Status)
			lastStatus = progress.Status
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading pull response: %w", err)
	}

	log.Info("model pulled successfully", "model", model)
	return nil
}

// ListModels queries the Ollama API for all locally available models.
func (m *OllamaManager) ListModels(ctx context.Context) ([]ModelInfo, error) {
	url := m.host + "/api/tags"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create list request: %w", err)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list models failed (is ollama running?): %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list models returned status %d: %s", resp.StatusCode, string(errBody))
	}

	var tagsResp struct {
		Models []struct {
			Name       string `json:"name"`
			Size       int64  `json:"size"`
			ModifiedAt string `json:"modified_at"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		return nil, fmt.Errorf("decode list response: %w", err)
	}

	models := make([]ModelInfo, 0, len(tagsResp.Models))
	for _, m := range tagsResp.Models {
		models = append(models, ModelInfo{
			Name:       m.Name,
			Size:       m.Size,
			ModifiedAt: m.ModifiedAt,
		})
	}

	return models, nil
}

// ---------------------------------------------------------------------------
// Status
// ---------------------------------------------------------------------------

// Status returns the current state of the Ollama runtime:
// "running", "stopped", or "not installed".
func (m *OllamaManager) Status(ctx context.Context) string {
	if !m.IsInstalled() {
		return "not installed"
	}

	if m.isHealthy(ctx) {
		return "running"
	}

	return "stopped"
}

// isHealthy performs a quick GET to /api/tags to check if Ollama is reachable.
func (m *OllamaManager) isHealthy(ctx context.Context) bool {
	url := m.host + "/api/tags"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode == http.StatusOK
}

// HasModel checks if a model is available locally.
func (m *OllamaManager) HasModel(ctx context.Context, model string) bool {
	models, err := m.ListModels(ctx)
	if err != nil {
		return false
	}
	base := strings.Split(strings.ToLower(model), ":")[0]
	for _, mi := range models {
		if strings.Split(strings.ToLower(mi.Name), ":")[0] == base {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Health monitor
// ---------------------------------------------------------------------------

// StartHealthMonitor begins a background goroutine that probes Ollama every
// 15 seconds. If Ollama dies and we started it, auto-restart with backoff.
func (m *OllamaManager) StartHealthMonitor() {
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.monitorCancel = cancel
	m.mu.Unlock()

	go m.healthMonitorLoop(ctx)
	log.Info("ollama health monitor started")
}

// StopHealthMonitor stops the background health monitor goroutine.
func (m *OllamaManager) StopHealthMonitor() {
	m.mu.Lock()
	cancel := m.monitorCancel
	m.monitorCancel = nil
	m.mu.Unlock()

	if cancel != nil {
		cancel()
		log.Info("ollama health monitor stopped")
	}
}

func (m *OllamaManager) healthMonitorLoop(ctx context.Context) {
	const probeInterval = 15 * time.Second
	const maxCrashes = 5
	backoff := 2 * time.Second
	backoffMax := 60 * time.Second
	consecutiveCrashes := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(probeInterval):
		}

		if m.isHealthy(ctx) {
			if consecutiveCrashes > 0 {
				log.Info("ollama recovered", "after_crashes", consecutiveCrashes)
				consecutiveCrashes = 0
				backoff = 2 * time.Second
			}
			continue
		}

		// Ollama is not healthy
		m.mu.Lock()
		weStarted := m.weStartedIt
		m.mu.Unlock()

		if !weStarted {
			log.Warn("ollama is unreachable (not managed by us - will not restart)")
			continue
		}

		consecutiveCrashes++
		log.Warn("ollama crashed", "consecutive", consecutiveCrashes)

		if consecutiveCrashes >= maxCrashes {
			log.Error("ollama exceeded max consecutive crashes - giving up", "max", maxCrashes)
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		log.Info("attempting ollama restart", "backoff", backoff)
		if err := m.Start(ctx); err != nil {
			log.Error("ollama restart failed", "error", err)
		}

		backoff = backoff * 2
		if backoff > backoffMax {
			backoff = backoffMax
		}
	}
}
