// Mini Krill - Your crustaceous AI buddy
package main

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/srvsngh99/mini-krill/internal/agent"
	"github.com/srvsngh99/mini-krill/internal/brain"
	"github.com/srvsngh99/mini-krill/internal/chat"
	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
	"github.com/srvsngh99/mini-krill/internal/llm"
	klog "github.com/srvsngh99/mini-krill/internal/log"
	"github.com/srvsngh99/mini-krill/internal/ollama"
	"github.com/srvsngh99/mini-krill/internal/plugin"
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
	Use:   "minikrill",
	Short: "Mini Krill - Your crustaceous AI buddy",
	Long: cBCyan + `
   ~~~~
  >=\'>    Mini Krill` + cReset + ` - Your crustaceous AI buddy
` + cBCyan + `   ~~~~` + cReset + `

  A lightweight, open-source AI agent that runs locally via
  Ollama or connects to cloud providers.

  ` + cDim + `Get started:` + cReset + `
    ` + cCyan + `minikrill init` + cReset + `       Setup wizard
    ` + cCyan + `minikrill chat` + cReset + `       Interactive chat
    ` + cCyan + `minikrill dive` + cReset + `       Start background services
    ` + cCyan + `minikrill tui` + cReset + `        Terminal dashboard
    ` + cCyan + `minikrill doctor` + cReset + `     Health diagnostics`,
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable debug logging")

	ollamaCmd.AddCommand(ollamaInstallCmd, ollamaStartCmd, ollamaStopCmd,
		ollamaPullCmd, ollamaListCmd, ollamaStatusCmd, ollamaEnsureCmd)
	skillCmd.AddCommand(skillListCmd)
	brainCmd.AddCommand(brainStatusCmd, brainRecallCmd, brainForgetCmd, brainSearchCmd)
	personalityCmd.AddCommand(personalityListCmd, personalityCreateCmd, personalitySwitchCmd, personalityShowCmd)

	rootCmd.AddCommand(initCmd, diveCmd, surfaceCmd, chatCmd, tuiCmd,
		doctorCmd, sonarCmd, versionCmd, ollamaCmd, skillCmd, brainCmd, personalityCmd)
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// krillStack holds all initialized subsystems.
type krillStack struct {
	cfg     *config.Config
	llm     core.LLMProvider
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

	krillBrain, err := brain.New(cfg.Brain, llmProvider)
	if err != nil {
		return nil, fmt.Errorf("init brain: %w", err)
	}

	skillReg := plugin.NewRegistry()
	skillReg.RegisterBuiltins()
	if cfg.Plugins.Dir != "" {
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
		LLM:       llmProvider,
		DataDir:   config.DataDir(),
	})

	krillAgent := agent.New(cfg.Agent, llmProvider, krillBrain, skillReg, mcpReg)
	chatHandler := chat.NewHandler(krillAgent)

	return &krillStack{
		cfg:     cfg,
		llm:     llmProvider,
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

func printBanner() {
	fmt.Println()
	fmt.Println(cBCyan + "   ~~~~" + cReset)
	fmt.Printf(cBCyan+"  >=\\'>"+cReset+"    "+cBold+"Mini Krill"+cReset+" v%s\n", core.Version)
	fmt.Println(cBCyan + "   ~~~~" + cReset + "    " + cDim + "Your crustaceous AI buddy" + cReset)
	fmt.Println()
}

func ask(scanner *bufio.Scanner, question string) string {
	fmt.Print(question)
	scanner.Scan()
	return strings.TrimSpace(scanner.Text())
}

func isConfirmation(s string) bool {
	s = strings.ToLower(s)
	return s == "yes" || s == "y" || s == "ok" || s == "sure" || s == "yep"
}

// friendlyError strips raw API error dumps into a short, helpful message.
func friendlyError(err error) string {
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

func spinner(done <-chan struct{}) {
	frames := []string{".", "..", "..."}
	i := 0
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	fmt.Print(cDimCyan + "  thinking" + cReset)
	for {
		select {
		case <-done:
			fmt.Print("\r\033[K")
			return
		case <-ticker.C:
			fmt.Print("\r" + cDimCyan + "  thinking" + frames[i%3] + cReset + "   ")
			i++
		}
	}
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
