package llm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
)

// idleTimeout is how long runCLI waits with zero output from the subprocess
// before considering it stuck and killing it. The total runtime is unlimited
// as long as the process keeps producing output.
const idleTimeout = 10 * time.Minute

// CLIProvider delegates model calls to official subscription-aware CLIs.
// It intentionally stores no OAuth tokens; Codex/Claude own authentication.
type CLIProvider struct {
	name  string
	model string
}

func NewCLIProvider(name string, cfg config.LLMConfig) *CLIProvider {
	model := cfg.Model
	if model == "" {
		model = "auto"
	}
	return &CLIProvider{name: name, model: model}
}

func (p *CLIProvider) Chat(ctx context.Context, messages []core.Message, opts ...core.ChatOption) (*core.Response, error) {
	options := core.ApplyOptions(opts)
	model := p.model
	if options.Model != "" {
		model = options.Model
	}

	prompt := renderCLIPrompt(messages, options.SystemPrompt)
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("%s provider received empty prompt", p.name)
	}

	var out []byte
	var err error
	switch p.name {
	case "codex":
		out, err = p.runCodex(ctx, model, prompt)
	case "claude":
		out, err = p.runClaude(ctx, model, prompt)
	default:
		return nil, fmt.Errorf("unsupported CLI provider %q", p.name)
	}
	if err != nil {
		return nil, err
	}

	return &core.Response{
		Content: strings.TrimSpace(string(out)),
		Model:   model,
	}, nil
}

func (p *CLIProvider) Stream(ctx context.Context, messages []core.Message, opts ...core.ChatOption) (<-chan core.StreamChunk, error) {
	ch := make(chan core.StreamChunk, 1)
	go func() {
		defer close(ch)
		resp, err := p.Chat(ctx, messages, opts...)
		if err != nil {
			ch <- core.StreamChunk{Done: true, Err: err}
			return
		}
		ch <- core.StreamChunk{Content: resp.Content, Done: true}
	}()
	return ch, nil
}

func (p *CLIProvider) Name() string { return p.name }

func (p *CLIProvider) ModelName() string { return p.model }

func (p *CLIProvider) Available(ctx context.Context) bool {
	switch p.name {
	case "codex":
		return commandOK(ctx, "codex", "login", "status")
	case "claude":
		return commandOK(ctx, "claude", "auth", "status")
	default:
		return false
	}
}

func (p *CLIProvider) runCodex(ctx context.Context, model, prompt string) ([]byte, error) {
	args := []string{
		"exec",
		"--sandbox", "workspace-write",
		"--skip-git-repo-check",
		"--ephemeral",
		"--color", "never",
	}
	if model != "" && model != "auto" {
		args = append(args, "--model", model)
	}
	args = append(args, "-")
	return runCLI(ctx, "codex", args, prompt)
}

func (p *CLIProvider) runClaude(ctx context.Context, model, prompt string) ([]byte, error) {
	args := []string{
		"--print",
		"--output-format", "text",
		"--permission-mode", "plan",
		"--no-session-persistence",
	}
	if model != "" && model != "auto" {
		args = append(args, "--model", model)
	}
	// Pass prompt via stdin to avoid exceeding OS ARG_MAX limits
	return runCLI(ctx, "claude", args, prompt)
}

// runCLI executes a CLI subprocess with idle-based termination. The process
// can run for as long as it needs — there is no wall-clock cap. But if it
// produces zero bytes on both stdout and stderr for idleTimeout, it is
// considered stuck and gets killed.
func runCLI(ctx context.Context, name string, args []string, stdin string) ([]byte, error) {
	// Create a child context we can cancel when idle timeout fires.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	// Suppress macOS dyld MallocStackLogging warnings at the source. The flag
	// is leaked into every Go-spawned subprocess on dev machines and pollutes
	// dive.log with thousands of "can't turn off malloc stack logging" lines.
	cmd.Env = append(os.Environ(), "MallocStackLogging=0")

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%s: stdout pipe: %w", name, err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("%s: stderr pipe: %w", name, err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s failed to start: %w", name, err)
	}

	// Idle timer — reset every time bytes arrive on either pipe.
	idle := time.NewTimer(idleTimeout)
	defer idle.Stop()

	var stdout, stderr bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)

	// readAndBump drains a pipe into a buffer, resetting the idle timer on
	// every chunk read. When the pipe closes (process exits or EOF), the
	// goroutine returns.
	readAndBump := func(pipe io.Reader, buf *bytes.Buffer) {
		defer wg.Done()
		tmp := make([]byte, 4096)
		for {
			n, err := pipe.Read(tmp)
			if n > 0 {
				buf.Write(tmp[:n])
				// Reset the idle timer — process is alive.
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(idleTimeout)
			}
			if err != nil {
				return
			}
		}
	}

	go readAndBump(stdoutPipe, &stdout)
	go readAndBump(stderrPipe, &stderr)

	// Wait for either: both pipes close (normal exit), idle timeout, or
	// parent context cancellation.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	var idled bool
	select {
	case <-done:
		// Process finished normally — pipes closed.
	case <-idle.C:
		// No output for idleTimeout — kill the stuck process.
		idled = true
		cancel()
	case <-ctx.Done():
		// Parent cancelled (user quit, app shutdown).
	}

	// Wait for the process to exit and the reader goroutines to drain.
	waitErr := cmd.Wait()
	wg.Wait()

	if idled {
		return nil, fmt.Errorf("%s idle for %v with no output — process killed", name, idleTimeout)
	}
	if waitErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = waitErr.Error()
		}
		return nil, fmt.Errorf("%s failed: %s", name, redactCLIError(msg))
	}
	return stdout.Bytes(), nil
}

func commandOK(ctx context.Context, name string, args ...string) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Run() == nil
}

func renderCLIPrompt(messages []core.Message, systemPrompt string) string {
	var b strings.Builder
	if systemPrompt != "" {
		b.WriteString("System:\n")
		b.WriteString(systemPrompt)
		b.WriteString("\n\n")
	}
	for _, m := range messages {
		role := strings.TrimSpace(m.Role)
		if role == "" {
			role = "user"
		}
		b.WriteString(titleRole(role))
		b.WriteString(":\n")
		b.WriteString(m.Content)
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

func redactCLIError(msg string) string {
	lines := strings.Split(msg, "\n")
	if len(lines) > 6 {
		lines = lines[:6]
	}
	msg = strings.Join(lines, "\n")
	if len(msg) > 700 {
		msg = msg[:700] + "..."
	}
	return msg
}

func titleRole(role string) string {
	if role == "" {
		return "User"
	}
	return strings.ToUpper(role[:1]) + role[1:]
}
