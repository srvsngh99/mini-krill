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
	"sync/atomic"
	"time"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
)

// idleTimeout is how long runCLI waits with zero output from the subprocess
// before considering it stuck and killing it. The total runtime is unlimited
// as long as the process keeps producing output. This is deliberately a
// fixed const rather than an LLMConfig field: it is a stuck-process backstop,
// not a tuning knob, and a per-provider value invites users to set it low
// enough to re-introduce the wall-clock-cap bug this change removes.
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
	return runCLIWithIdle(ctx, name, args, stdin, idleTimeout)
}

// runCLIWithIdle is the implementation of runCLI with an injectable idle
// duration. Production code always passes the fixed idleTimeout const via
// runCLI; the parameter exists only so tests can shrink it to drive the
// idle-kill and slow-stream paths under the race detector.
func runCLIWithIdle(ctx context.Context, name string, args []string, stdin string, idleAfter time.Duration) ([]byte, error) {
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

	var stdout, stderr bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)

	// bump carries an "I saw output" signal from the reader goroutines to the
	// single timer-owning monitor goroutine. Buffered+non-blocking send: the
	// readers must never block on a slow/absent monitor, and one queued bump is
	// enough — the monitor only needs to know *that* there was activity, not
	// how much.
	bump := make(chan struct{}, 1)

	// readAndBump drains a pipe into a buffer, signalling activity on every
	// chunk read. When the pipe closes (process exits or EOF), it returns.
	readAndBump := func(pipe io.Reader, buf *bytes.Buffer) {
		defer wg.Done()
		tmp := make([]byte, 4096)
		for {
			n, err := pipe.Read(tmp)
			if n > 0 {
				buf.Write(tmp[:n])
				select {
				case bump <- struct{}{}:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}

	go readAndBump(stdoutPipe, &stdout)
	go readAndBump(stderrPipe, &stderr)

	// done closes once both reader goroutines have returned, i.e. both pipes
	// are fully drained and closed. Nothing reads the pipes after this.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	// Single owner of the idle timer. Resets on every activity bump; on
	// idleTimeout with no bump it cancels the context (killing the stuck
	// process, which closes the pipes and unblocks the readers).
	var idled atomic.Bool
	go func() {
		idle := time.NewTimer(idleAfter)
		defer idle.Stop()
		for {
			select {
			case <-bump:
				// Go 1.23+: Reset alone is correct; no manual drain needed,
				// and a stale fire is never delivered after Reset.
				idle.Reset(idleAfter)
			case <-idle.C:
				idled.Store(true)
				cancel()
				return
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// Block until the readers have fully drained the pipes (normal exit, idle
	// kill, or parent cancellation all converge here once the process dies and
	// its pipes close). Only then is it safe to call cmd.Wait(), which closes
	// the pipe FDs — calling it while a reader's pipe.Read is in flight is a
	// documented data race.
	<-done
	waitErr := cmd.Wait()

	switch {
	case idled.Load():
		return nil, fmt.Errorf("%s idle for %v with no output — process killed", name, idleAfter)
	case ctx.Err() != nil:
		// Parent cancelled deliberately (user quit TUI, app shutdown).
		// Surface the real cause rather than a generic "<killed>" string.
		return nil, fmt.Errorf("%s cancelled: %w", name, ctx.Err())
	case waitErr != nil:
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
