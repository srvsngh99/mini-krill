// Mini Krill - Your crustaceous AI buddy
package main

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/srvsngh99/mini-krill/internal/agent"
	"github.com/srvsngh99/mini-krill/internal/brain"
	"github.com/srvsngh99/mini-krill/internal/brand"
	"github.com/srvsngh99/mini-krill/internal/chat"
	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
	"github.com/srvsngh99/mini-krill/internal/llm"
	klog "github.com/srvsngh99/mini-krill/internal/log"
	"github.com/srvsngh99/mini-krill/internal/ollama"
	"github.com/srvsngh99/mini-krill/internal/plugin"
	"github.com/srvsngh99/mini-krill/internal/reminder"
)

// ANSI color codes - ocean palette
const (
	cReset   = "\033[0m"
	cBold    = "\033[1m"
	cDim     = "\033[2m"
	cCyan    = "\033[36m"
	cGreen   = "\033[32m"
	cYellow  = "\033[33m"
	cRed     = "\033[31m"
	cBlue    = "\033[34m"
	cMagenta = "\033[35m"
	cBCyan   = "\033[1;36m"
	cBGreen  = "\033[1;32m"
	cBYellow = "\033[1;33m"
	cBRed    = "\033[1;31m"
	cBBlue   = "\033[1;34m"
	cDimCyan = "\033[2;36m"
)

var verbose bool

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:     "minikrill",
	Short:   "mini-krill — a lightweight, local-first AI agent",
	Version: core.Version,
	Long: "  " + cBold + brand.Wordmark + cReset + "   " + cDim + brand.Tagline + cReset + `

  A lightweight, open-source AI agent that runs locally via
  Ollama or through subscription-backed Codex/Claude CLIs.

  ` + cDim + `Get started:` + cReset + `
    ` + cBold + `minikrill init` + cReset + `       Setup wizard
    ` + cBold + `minikrill chat` + cReset + `       Interactive chat
    ` + cBold + `minikrill dive` + cReset + `       Start background services
    ` + cBold + `minikrill tui` + cReset + `        Terminal dashboard
    ` + cBold + `minikrill doctor` + cReset + `     Health diagnostics

  ` + cDim + `Documentation:` + cReset + `
    ` + cBold + `README.md` + cReset + `            Overview and usage
    ` + cBold + `docs/INSTALL.md` + cReset + `     Install and setup
    ` + cBold + `docs/PROVIDERS.md` + cReset + `   Ollama, Codex, Claude
    ` + cBold + `docs/MEMORY.md` + cReset + `      Memory and preferences
    ` + cBold + `docs/INTERFACES.md` + cReset + `  CLI, Telegram, Discord
    ` + cBold + `docs/TESTING.md` + cReset + `     Feature test checklist

  ` + cDim + brand.LabMark + "  " + brand.Lab + "  ·  " + brand.Site + cReset,
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable debug logging")

	ollamaCmd.AddCommand(ollamaInstallCmd, ollamaStartCmd, ollamaStopCmd,
		ollamaPullCmd, ollamaListCmd, ollamaStatusCmd, ollamaEnsureCmd)
	skillCmd.AddCommand(skillListCmd)
	brainCmd.AddCommand(brainStatusCmd, brainRecallCmd, brainForgetCmd, brainSearchCmd)
	personalityCmd.AddCommand(personalityListCmd, personalityCreateCmd, personalitySwitchCmd, personalityShowCmd)

	rootCmd.AddCommand(initCmd, diveCmd, surfaceCmd, chatCmd, tuiCmd,
		doctorCmd, sonarCmd, versionCmd, ollamaCmd, skillCmd, brainCmd, personalityCmd,
		runCmd, notifyCmd, remindCmd, remindersCmd, summarizeCmd, webCmd, researchCmd, youtubeCmd)
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// krillStack holds all initialized subsystems.
type krillStack struct {
	cfg     *config.Config
	llm     *llm.ProviderManager
	brain   *brain.KrillBrain
	hb      core.Heartbeat
	skills  *plugin.SkillRegistryImpl
	mcp     *plugin.MCPRegistryImpl
	agent   *agent.KrillAgent
	handler *chat.ChatHandlerImpl
}

// initStack boots every subsystem needed for chat/dive/tui.
func initStack(quiet bool) (*krillStack, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	if verbose {
		cfg.Log.Level = "debug"
		quiet = false
	}
	if err := config.EnsureDataDir(); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	if quiet {
		_ = klog.InitQuiet(cfg.Log)
	} else {
		_ = klog.Init(cfg.Log)
	}

	if cfg.Agent.Personality != "" {
		cfg.Brain.Personality = cfg.Agent.Personality
	}

	llmProvider, err := llm.NewProvider(cfg.LLM, cfg.Ollama)
	if err != nil {
		return nil, fmt.Errorf("init LLM provider: %w", err)
	}
	providerMgr := llm.NewProviderManager(cfg, llmProvider)

	krillBrain, err := brain.New(cfg.Brain, providerMgr)
	if err != nil {
		return nil, fmt.Errorf("init brain: %w", err)
	}

	skillReg := plugin.NewRegistry()
	skillReg.RegisterBuiltins()
	if cfg.Plugins.Dir != "" {
		plugin.SeedDefaultSkills(cfg.Plugins.Dir)
		_ = skillReg.LoadSkillsFromDir(cfg.Plugins.Dir)
	}

	mcpReg := plugin.NewMCPRegistry()
	if len(cfg.MCP.Servers) > 0 {
		_ = mcpReg.LoadFromConfig(cfg.MCP.Servers)
	}

	skillReg.RegisterSelfSkills(plugin.SelfContext{
		Brain:     krillBrain,
		Config:    cfg,
		Heartbeat: krillBrain.Heartbeat(),
		Skills:    skillReg,
		LLM:       providerMgr,
		DataDir:   config.DataDir(),
	})
	// Read-only "eyes on self" skills: read source + tail logs.
	skillReg.RegisterSelfEyes()

	// Register feature skills (YouTube, reminders, web, research, notify)
	reminderStore, err := reminder.NewStore(filepath.Join(config.DataDir(), "reminders.jsonl"))
	if err != nil {
		klog.Warn("reminder store init failed, remind skill will not be available", "error", err)
	}
	skillReg.RegisterFeatureSkills(plugin.FeatureContext{
		Config:    cfg,
		Reminders: reminderStore,
	})

	// Pass recovery config from brain to agent for cold-start continuity
	cfg.Agent.RecoveryTurns = cfg.Brain.RecoveryTurns

	krillAgent := agent.New(cfg.Agent, providerMgr, krillBrain, skillReg, mcpReg)
	// Tasks are orchestration state, not memories — keep them out of the brain dir
	// so clearing memory doesn't wipe in-flight task records.
	krillAgent.InitTaskSystem(filepath.Join(config.DataDir(), "tasks"), 3)
	chatHandler := chat.NewHandler(krillAgent, reminderStore)

	return &krillStack{
		cfg:     cfg,
		llm:     providerMgr,
		brain:   krillBrain,
		hb:      krillBrain.Heartbeat(),
		skills:  skillReg,
		mcp:     mcpReg,
		agent:   krillAgent,
		handler: chatHandler,
	}, nil
}

// loadConfigWithLog loads config and inits quiet logging.
func loadConfigWithLog() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	_ = klog.InitQuiet(cfg.Log)
	return cfg, nil
}

// newOllamaManager loads config and creates an OllamaManager.
func newOllamaManager() (*ollama.OllamaManager, *config.Config) {
	cfg, _ := config.Load()
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	return ollama.NewManager(cfg.Ollama), cfg
}

// printBanner prints the monochrome mini-krill lockup: the `>_ mini-krill`
// wordmark in bold, the tagline and Sourav AI Labs lockup dimmed beneath.
func printBanner() {
	fmt.Println()
	fmt.Printf("  %s%s%s   %sv%s%s\n", cBold, brand.Wordmark, cReset, cDim, core.Version, cReset)
	fmt.Printf("  %s%s%s\n", cDim, brand.Tagline, cReset)
	fmt.Printf("  %s%s  %s  ·  %s%s\n", cDim, brand.LabMark, brand.Lab, brand.Site, cReset)
	fmt.Println()
}

func ask(scanner *bufio.Scanner, question string) string {
	fmt.Print(question)
	scanner.Scan()
	return strings.TrimSpace(scanner.Text())
}

// capitalizeFirstASCII uppercases the first ASCII letter of s. Used in the
// init wizard to render personality slugs ("buddy" → "Buddy") without the
// deprecated strings.Title or pulling in golang.org/x/text/cases for a
// single-character upcase.
func capitalizeFirstASCII(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r)
}

// friendlyError strips raw API error dumps into a short, helpful message.
func friendlyError(err error) string {
	if err == nil {
		return "not started"
	}
	msg := err.Error()
	if idx := strings.Index(msg, `{"error"`); idx > 0 {
		msg = msg[:idx]
	}
	if idx := strings.Index(msg, `{"message"`); idx > 0 {
		msg = msg[:idx]
	}
	msg = strings.TrimRight(msg, " \n\t:,")
	if len(msg) > 120 {
		msg = msg[:117] + "..."
	}
	if msg == "" {
		msg = "Something went wrong in the deep"
	}
	return msg
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func randomFact() string {
	return core.KrillFacts[rand.Intn(len(core.KrillFacts))]
}
