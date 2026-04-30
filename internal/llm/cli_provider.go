package llm

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
)

// CLIProvider delegates model calls to official subscription-aware CLIs.
// It intentionally stores no OAuth tokens; Codex/Claude own authentication.
type CLIProvider struct {
	name    string
	model   string
	timeout time.Duration
}

func NewCLIProvider(name string, cfg config.LLMConfig) *CLIProvider {
	model := cfg.Model
	if model == "" {
		model = "auto"
	}
	return &CLIProvider{name: name, model: model, timeout: 10 * time.Minute}
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

	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

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
		"--sandbox", "read-only",
		"--ask-for-approval", "never",
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
	args = append(args, prompt)
	return runCLI(ctx, "claude", args, "")
}

func runCLI(ctx context.Context, name string, args []string, stdin string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s failed: %s", name, redactCLIError(msg))
	}
	return out, nil
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
