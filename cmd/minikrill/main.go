// Mini Krill - Your crustaceous AI buddy
// A lightweight, open-source AI agent inspired by DeepKrill.
// Krill fact: krill have survived for 130 million years - this binary aims for the same uptime.
package main

import (
	"bufio"
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/srvsngh99/mini-krill/internal/agent"
	"github.com/srvsngh99/mini-krill/internal/brain"
	"github.com/srvsngh99/mini-krill/internal/chat"
	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
	"github.com/srvsngh99/mini-krill/internal/doctor"
	"github.com/srvsngh99/mini-krill/internal/llm"
	klog "github.com/srvsngh99/mini-krill/internal/log"
	"github.com/srvsngh99/mini-krill/internal/ollama"
	"github.com/srvsngh99/mini-krill/internal/plugin"
	"github.com/srvsngh99/mini-krill/internal/tui"
)

// ---------------------------------------------------------------------------
// ANSI color codes - ocean palette
// ---------------------------------------------------------------------------

const (
	cReset  = "\033[0m"
	cBold   = "\033[1m"
	cDim    = "\033[2m"
	cCyan   = "\033[36m"
	cGreen  = "\033[32m"
	cYellow = "\033[33m"
	cRed    = "\033[31m"
	cBlue   = "\033[34m"
	cMagenta = "\033[35m"
	cBCyan  = "\033[1;36m" // bold cyan
	cBGreen = "\033[1;32m" // bold green
	cBYellow = "\033[1;33m"
	cBRed   = "\033[1;31m"
	cBBlue  = "\033[1;34m"
	cDimCyan = "\033[2;36m"
)

var verbose bool

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// Root command
// ---------------------------------------------------------------------------

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
		ollamaPullCmd, ollamaListCmd, ollamaStatusCmd)
	skillCmd.AddCommand(skillListCmd)
	brainCmd.AddCommand(brainStatusCmd, brainRecallCmd, brainForgetCmd, brainSearchCmd)
	personalityCmd.AddCommand(personalityListCmd, personalityCreateCmd, personalitySwitchCmd, personalityShowCmd)

	rootCmd.AddCommand(initCmd, diveCmd, surfaceCmd, chatCmd, tuiCmd,
		doctorCmd, sonarCmd, versionCmd, ollamaCmd, skillCmd, brainCmd, personalityCmd)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func printBanner() {
	fmt.Println()
	fmt.Println(cBCyan + "   ~~~~" + cReset)
	fmt.Printf(cBCyan+"  >=\\'>"+cReset+"    "+cBold+"Mini Krill"+cReset+" v%s\n", core.Version)
	fmt.Println(cBCyan + "   ~~~~" + cReset + "    " + cDim + "Your crustaceous AI buddy" + cReset)
	fmt.Println()
}

func printChatHelp() {
	fmt.Println()
	fmt.Println(cBCyan + "  Commands:" + cReset)
	fmt.Println("    " + cCyan + "help" + cReset + "      Show this help")
	fmt.Println("    " + cCyan + "fact" + cReset + "      Random krill fact")
	fmt.Println("    " + cCyan + "status" + cReset + "    System status")
	fmt.Println("    " + cCyan + "exit" + cReset + "      Leave the chat")
	fmt.Println()
	fmt.Println(cBCyan + "  Tips:" + cReset)
	fmt.Println(cDim + "    Ask anything" + cReset + " - I'll chat naturally")
	fmt.Println(cDim + "    Give me a task" + cReset + " - I'll show a dive plan for your approval")
	fmt.Println(cDim + "    Say" + cReset + " 'yes'" + cDim + " to approve," + cReset + " 'no'" + cDim + " to reject a plan" + cReset)
	fmt.Println()
}

func ask(scanner *bufio.Scanner, question string) string {
	fmt.Print(question)
	scanner.Scan()
	return strings.TrimSpace(scanner.Text())
}

// isConfirmation returns true if the input is just confirming the default.
// This prevents "yes" from being used as a model name etc.
func isConfirmation(s string) bool {
	s = strings.ToLower(s)
	return s == "yes" || s == "y" || s == "ok" || s == "sure" || s == "yep"
}

func colored(color, text string) string {
	return color + text + cReset
}

// friendlyError strips raw API error dumps into a short, helpful message.
func friendlyError(err error) string {
	msg := err.Error()
	// Strip long JSON bodies
	if idx := strings.Index(msg, `{"error"`); idx > 0 {
		msg = msg[:idx]
	}
	if idx := strings.Index(msg, `{"message"`); idx > 0 {
		msg = msg[:idx]
	}
	// Trim trailing whitespace/punctuation
	msg = strings.TrimRight(msg, " \n\t:,")
	// Keep it short
	if len(msg) > 120 {
		msg = msg[:117] + "..."
	}
	if msg == "" {
		msg = "Something went wrong in the deep"
	}
	return msg
}

// spinner shows a simple dots animation while waiting.
func spinner(done <-chan struct{}) {
	frames := []string{".", "..", "..."}
	i := 0
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	fmt.Print(cDimCyan + "  thinking" + cReset)
	for {
		select {
		case <-done:
			fmt.Print("\r\033[K") // clear line
			return
		case <-ticker.C:
			fmt.Print("\r" + cDimCyan + "  thinking" + frames[i%3] + cReset + "   ")
			i++
		}
	}
}

// initStack boots every subsystem needed for chat/dive/tui.
// quiet=true suppresses stderr logging (for interactive modes).
func initStack(quiet bool) (*krillStack, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	if verbose {
		cfg.Log.Level = "debug"
		quiet = false // verbose overrides quiet
	}
	if err := config.EnsureDataDir(); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	if quiet {
		_ = klog.InitQuiet(cfg.Log)
	} else {
		_ = klog.Init(cfg.Log)
	}

	// Sync personality from agent config to brain config
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

	// Self-awareness: give the krill eyes and hands on its own internals
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

// ---------------------------------------------------------------------------
// init command - setup wizard
// ---------------------------------------------------------------------------

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Setup wizard - configure Mini Krill",
	RunE: func(cmd *cobra.Command, args []string) error {
		printBanner()
		fmt.Println(cBold + "  Welcome to Mini Krill setup!" + cReset)
		fmt.Println(cDim + "  Let's get you swimming in under a minute." + cReset)
		fmt.Println()

		cfg := config.DefaultConfig()
		scanner := bufio.NewScanner(os.Stdin)

		// LLM provider selection
		fmt.Println(cBCyan + "  Choose your LLM provider:" + cReset)
		fmt.Println()
		fmt.Println("    " + cBGreen + "[1]" + cReset + " Ollama        " + cDim + "local, free, private" + cReset)
		fmt.Println("    " + cBBlue + "[2]" + cReset + " OpenAI        " + cDim + "$20/mo plan" + cReset)
		fmt.Println("    " + cBBlue + "[3]" + cReset + " Anthropic     " + cDim + "$20/mo plan" + cReset)
		fmt.Println("    " + cBBlue + "[4]" + cReset + " Google Gemini " + cDim + "free tier available" + cReset)
		fmt.Println()
		choice := ask(scanner, cCyan+"  > "+cReset)

		switch choice {
		case "2", "openai":
			cfg.LLM.Provider = "openai"
			cfg.LLM.Model = "gpt-4o-mini"
			fmt.Println()
			cfg.LLM.APIKey = ask(scanner, cCyan+"  API key: "+cReset)
			fmt.Printf(cDim+"  Model [%s]: "+cReset, cfg.LLM.Model)
			scanner.Scan()
			if m := strings.TrimSpace(scanner.Text()); m != "" && !isConfirmation(m) {
				cfg.LLM.Model = m
			}
		case "3", "anthropic":
			cfg.LLM.Provider = "anthropic"
			cfg.LLM.Model = "claude-sonnet-4-20250514"
			fmt.Println()
			cfg.LLM.APIKey = ask(scanner, cCyan+"  API key: "+cReset)
			fmt.Printf(cDim+"  Model [%s]: "+cReset, cfg.LLM.Model)
			scanner.Scan()
			if m := strings.TrimSpace(scanner.Text()); m != "" && !isConfirmation(m) {
				cfg.LLM.Model = m
			}
		case "4", "google":
			cfg.LLM.Provider = "google"
			cfg.LLM.Model = "gemini-2.0-flash"
			fmt.Println()
			cfg.LLM.APIKey = ask(scanner, cCyan+"  API key: "+cReset)
			fmt.Printf(cDim+"  Model [%s]: "+cReset, cfg.LLM.Model)
			scanner.Scan()
			if m := strings.TrimSpace(scanner.Text()); m != "" && !isConfirmation(m) {
				cfg.LLM.Model = m
			}
		default: // 1, ollama, or empty
			cfg.LLM.Provider = "ollama"
			fmt.Println()
			mgr := ollama.NewManager(cfg.Ollama)
			if !mgr.IsInstalled() {
				ans := ask(scanner, cYellow+"  Ollama not found."+cReset+" Install now? "+cDim+"[Y/n]"+cReset+" ")
				if ans == "" || strings.HasPrefix(strings.ToLower(ans), "y") {
					fmt.Println(cDim + "  Installing Ollama..." + cReset)
					if err := mgr.Install(context.Background()); err != nil {
						fmt.Printf(cRed+"  Install failed: %v\n"+cReset, err)
						fmt.Println(cDim + "  Install manually: https://ollama.com" + cReset)
					} else {
						fmt.Println(cGreen + "  Ollama installed!" + cReset)
					}
				}
			} else {
				fmt.Println(cGreen + "  Ollama: found!" + cReset)
			}
			fmt.Printf(cDim+"  Model [%s]: "+cReset, cfg.Ollama.DefaultModel)
			scanner.Scan()
			if m := strings.TrimSpace(scanner.Text()); m != "" && !isConfirmation(m) {
				cfg.LLM.Model = m
				cfg.Ollama.DefaultModel = m
			}
		}

		// Telegram
		fmt.Println()
		if ans := ask(scanner, cCyan+"  Enable Telegram bot? "+cDim+"[y/N]"+cReset+" "); strings.HasPrefix(strings.ToLower(ans), "y") {
			cfg.Telegram.Token = ask(scanner, cCyan+"  Bot token: "+cReset)
			cfg.Telegram.Enabled = cfg.Telegram.Token != ""
		}

		// Discord
		if ans := ask(scanner, cCyan+"  Enable Discord bot? "+cDim+"[y/N]"+cReset+" "); strings.HasPrefix(strings.ToLower(ans), "y") {
			cfg.Discord.Token = ask(scanner, cCyan+"  Bot token: "+cReset)
			cfg.Discord.Enabled = cfg.Discord.Token != ""
		}

		// Save
		if err := config.EnsureDataDir(); err != nil {
			return fmt.Errorf("create data dir: %w", err)
		}
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		fmt.Println()
		fmt.Println(cBGreen + "  Mini Krill is ready to dive!" + cReset)
		fmt.Printf(cDim+"  Config: %s\n"+cReset, filepath.Join(config.DataDir(), "config.yaml"))
		fmt.Println()
		fmt.Printf(cDimCyan+"  Fun fact: %s\n"+cReset, core.KrillFacts[rand.Intn(len(core.KrillFacts))])
		fmt.Println()
		fmt.Println(cBold + "  Next steps:" + cReset)
		fmt.Println("    " + cCyan + "minikrill chat" + cReset + "     Start chatting")
		fmt.Println("    " + cCyan + "minikrill tui" + cReset + "      Terminal dashboard")
		fmt.Println("    " + cCyan + "minikrill doctor" + cReset + "   Health check")
		fmt.Println()
		return nil
	},
}

// ---------------------------------------------------------------------------
// dive command - start all services
// ---------------------------------------------------------------------------

var diveCmd = &cobra.Command{
	Use:   "dive",
	Short: "Start Mini Krill - dive into the deep",
	Long:  "Starts the agent with all configured services (Telegram, Discord).",
	RunE: func(cmd *cobra.Command, args []string) error {
		stack, err := initStack(false)
		if err != nil {
			return err
		}

		printBanner()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if err := stack.hb.Start(ctx); err != nil {
			klog.Warn("heartbeat start failed", "error", err)
		}

		fmt.Printf("  "+cDim+"Provider:"+cReset+" %s "+cDim+"|"+cReset+" %s\n", stack.cfg.LLM.Provider, stack.cfg.LLM.Model)
		fmt.Printf("  "+cDim+"Brain:"+cReset+"    %d memories\n", stack.brain.Memory().Count())
		fmt.Printf("  "+cDim+"Skills:"+cReset+"   %d registered\n", len(stack.skills.List()))

		if stack.cfg.Telegram.Enabled {
			tgBot, err := chat.NewTelegramBot(stack.cfg.Telegram, stack.handler)
			if err != nil {
				fmt.Printf("  "+cRed+"Telegram: %s\n"+cReset, friendlyError(err))
			} else {
				// Wire group learning - krill remembers interesting group exchanges
				tgBot.SetLearnFunc(func(ctx context.Context, key, value string) error {
					return stack.brain.Memory().Store(ctx, core.MemoryEntry{
						Key:        key,
						Value:      value,
						Tags:       []string{"group-learned", "telegram"},
						CreatedAt:  time.Now(),
						AccessedAt: time.Now(),
					})
				})
				go func() {
					if err := tgBot.Start(ctx); err != nil {
						klog.Error("telegram error", "error", err)
					}
				}()
				fmt.Println("  " + cGreen + "Telegram: swimming" + cReset)
			}
		}
		if stack.cfg.Discord.Enabled {
			dcBot, err := chat.NewDiscordBot(stack.cfg.Discord, stack.handler)
			if err != nil {
				fmt.Printf("  "+cRed+"Discord: %s\n"+cReset, friendlyError(err))
			} else {
				go func() {
					if err := dcBot.Start(ctx); err != nil {
						klog.Error("discord error", "error", err)
					}
				}()
				fmt.Println("  " + cGreen + "Discord:  swimming" + cReset)
			}
		}

		pidFile := filepath.Join(config.DataDir(), "krill.pid")
		_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
		defer os.Remove(pidFile)

		fmt.Println()
		fmt.Printf(cBGreen+"  Mini Krill is swimming!"+cReset+" "+cDim+"(PID: %d)\n"+cReset, os.Getpid())
		fmt.Println(cDim + "  Press Ctrl+C to surface..." + cReset)
		fmt.Println()
		fmt.Println(cDimCyan + "  " + core.KrillFacts[rand.Intn(len(core.KrillFacts))] + cReset)

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		fmt.Println()
		fmt.Println(cCyan + "  Mini Krill is surfacing..." + cReset)
		cancel()
		_ = stack.hb.Stop()
		_ = stack.mcp.Close()
		return nil
	},
}

// ---------------------------------------------------------------------------
// surface command - stop running instance
// ---------------------------------------------------------------------------

var surfaceCmd = &cobra.Command{
	Use:   "surface",
	Short: "Stop a running Mini Krill instance",
	RunE: func(cmd *cobra.Command, args []string) error {
		pidFile := filepath.Join(config.DataDir(), "krill.pid")
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return fmt.Errorf("Mini Krill is not diving (no PID file)")
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			return fmt.Errorf("corrupt PID file")
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("process %d not found", pid)
		}
		if err := proc.Signal(syscall.SIGTERM); err != nil {
			_ = proc.Kill()
		}
		_ = os.Remove(pidFile)
		fmt.Println(cCyan + "Mini Krill is surfacing..." + cReset)
		return nil
	},
}

// ---------------------------------------------------------------------------
// chat command - interactive terminal chat
// ---------------------------------------------------------------------------

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Interactive chat with Mini Krill",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Quiet mode: no log spam in the chat UI
		stack, err := initStack(true)
		if err != nil {
			return err
		}

		printBanner()

		// Status line
		fmt.Printf(cDim+"  Provider: %s/%s"+cReset, stack.cfg.LLM.Provider, stack.cfg.LLM.Model)
		fmt.Printf(cDim+" | Brain: %d memories"+cReset, stack.brain.Memory().Count())
		fmt.Printf(cDim+" | Skills: %d"+cReset+"\n", len(stack.skills.List()))
		fmt.Println()

		// Greeting
		greeting := stack.brain.GetPersonality().Greeting
		if greeting == "" {
			greeting = "Hey there! I'm Mini Krill, your crustaceous AI buddy."
		}
		fmt.Println(cBCyan + "  >=\\'>" + cReset + " " + greeting)
		fmt.Println()

		// Hints
		fmt.Println(cDim + "  Type " + cReset + "help" + cDim + " for commands, " + cReset + "exit" + cDim + " to leave" + cReset)
		fmt.Println(cDim + "  Give me a task and I'll plan before doing anything" + cReset)
		fmt.Println()

		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 0), 1024*1024)
		ctx := context.Background()

		for {
			fmt.Print(cBGreen + "you > " + cReset)
			if !scanner.Scan() {
				break
			}
			input := strings.TrimSpace(scanner.Text())
			if input == "" {
				continue
			}
			switch strings.ToLower(input) {
			case "exit", "quit":
				fmt.Println()
				fmt.Println(cCyan + "  See you in the deep!" + cReset)
				fmt.Println()
				return nil
			case "help":
				printChatHelp()
				continue
			case "fact":
				fmt.Println()
				fmt.Printf(cBCyan+"krill > "+cReset+cDimCyan+"%s\n"+cReset, core.KrillFacts[rand.Intn(len(core.KrillFacts))])
				fmt.Println()
				continue
			case "status":
				s := stack.hb.Status()
				fmt.Println()
				fmt.Print(cBCyan + "krill > " + cReset)
				fmt.Printf(cDim+"Uptime: "+cReset+"%s", s.Uptime.Truncate(time.Second))
				fmt.Printf(cDim+" | Memory: "+cReset+"%d KB", s.MemoryUsed/1024)
				fmt.Printf(cDim+" | LLM: "+cReset+"%s", s.LLMStatus)
				fmt.Println()
				fmt.Println()
				continue
			}

			// Show thinking spinner
			done := make(chan struct{})
			go spinner(done)

			resp, err := stack.agent.Chat(ctx, input)
			close(done)

			fmt.Println()
			if err != nil {
				fmt.Printf(cBCyan+"krill > "+cReset+cYellow+"Bubbles! %s\n"+cReset, friendlyError(err))
				fmt.Println(cDim + "         Try rephrasing, or check 'minikrill doctor' for issues." + cReset)
			} else if resp == "" {
				fmt.Println(cBCyan + "krill > " + cReset + cDimCyan + core.KrillFacts[rand.Intn(len(core.KrillFacts))] + cReset)
			} else {
				fmt.Println(cBCyan + "krill > " + cReset + renderMarkdown(resp))
			}
			fmt.Println()
		}
		return nil
	},
}

// ---------------------------------------------------------------------------
// tui command - terminal dashboard
// ---------------------------------------------------------------------------

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch the terminal dashboard",
	RunE: func(cmd *cobra.Command, args []string) error {
		stack, err := initStack(true)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_ = stack.hb.Start(ctx)
		defer stack.hb.Stop()

		app := tui.NewApp(stack.agent, stack.brain, stack.hb, core.Version, stack.cfg.Log.File)
		return app.Run()
	},
}

// ---------------------------------------------------------------------------
// doctor command - health checks
// ---------------------------------------------------------------------------

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run health diagnostics",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			fmt.Printf(cBRed+"[FAIL]"+cReset+" config - %v\n", err)
			return nil
		}
		_ = klog.InitQuiet(cfg.Log)

		llmProvider, _ := llm.NewProvider(cfg.LLM, cfg.Ollama)
		doc := doctor.NewDoctor(cfg.Ollama.Host, llmProvider, cfg.Brain.DataDir)

		printBanner()
		fmt.Println(cDim + "  Running diagnostics..." + cReset)
		fmt.Println()
		results := doc.RunAll(context.Background())
		fmt.Println(doctor.FormatResults(results))

		ok, warn, fail := 0, 0, 0
		for _, r := range results {
			switch r.Status {
			case "ok":
				ok++
			case "warn":
				warn++
			case "fail":
				fail++
			}
		}
		fmt.Println()
		fmt.Printf("  %s%d passed%s, %s%d warnings%s, %s%d failed%s\n",
			cGreen, ok, cReset, cYellow, warn, cReset, cRed, fail, cReset)
		if fail > 0 {
			fmt.Println(cDim + "  Run " + cReset + "minikrill init" + cDim + " to reconfigure." + cReset)
		}
		fmt.Println()
		return nil
	},
}

// ---------------------------------------------------------------------------
// sonar command - quick health ping
// ---------------------------------------------------------------------------

var sonarCmd = &cobra.Command{
	Use:   "sonar",
	Short: "Quick health ping",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			fmt.Println(cRed + "FAIL" + cReset + " - config not loadable")
			return nil
		}
		_ = klog.InitQuiet(cfg.Log)
		llmProvider, err := llm.NewProvider(cfg.LLM, cfg.Ollama)
		if err != nil {
			fmt.Printf(cRed+"FAIL"+cReset+" - LLM: %s\n", friendlyError(err))
			return nil
		}
		if llmProvider.Available(context.Background()) {
			fmt.Printf(cBGreen+"PONG"+cReset+" - Mini Krill is alive! "+cDim+"(%s/%s)\n"+cReset, cfg.LLM.Provider, cfg.LLM.Model)
		} else {
			fmt.Printf(cBRed+"FAIL"+cReset+" - LLM not reachable "+cDim+"(%s/%s)\n"+cReset, cfg.LLM.Provider, cfg.LLM.Model)
		}
		return nil
	},
}

// ---------------------------------------------------------------------------
// version command
// ---------------------------------------------------------------------------

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf(cBold+"Mini Krill"+cReset+" v%s\n", core.Version)
		fmt.Printf(cDim+"Go:      "+cReset+"%s\n", runtime.Version())
		fmt.Printf(cDim+"OS/Arch: "+cReset+"%s/%s\n", runtime.GOOS, runtime.GOARCH)
		fmt.Println()
		fmt.Println(cDimCyan + core.KrillFacts[rand.Intn(len(core.KrillFacts))] + cReset)
	},
}

// ---------------------------------------------------------------------------
// ollama subcommands
// ---------------------------------------------------------------------------

var ollamaCmd = &cobra.Command{
	Use:   "ollama",
	Short: "Manage local Ollama installation",
}

var ollamaInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Ollama",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.Load()
		if cfg == nil {
			cfg = config.DefaultConfig()
		}
		mgr := ollama.NewManager(cfg.Ollama)
		if mgr.IsInstalled() {
			fmt.Println(cGreen + "Ollama is already installed!" + cReset)
			return nil
		}
		fmt.Println(cDim + "Installing Ollama..." + cReset)
		if err := mgr.Install(context.Background()); err != nil {
			return fmt.Errorf("install failed: %w\nInstall manually: https://ollama.com", err)
		}
		fmt.Println(cGreen + "Ollama installed!" + cReset)
		return nil
	},
}

var ollamaStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start Ollama server",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.Load()
		if cfg == nil {
			cfg = config.DefaultConfig()
		}
		mgr := ollama.NewManager(cfg.Ollama)
		fmt.Println(cDim + "Starting Ollama..." + cReset)
		if err := mgr.Start(context.Background()); err != nil {
			return err
		}
		fmt.Println(cGreen + "Ollama is running!" + cReset)
		return nil
	},
}

var ollamaStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop Ollama server",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.Load()
		if cfg == nil {
			cfg = config.DefaultConfig()
		}
		mgr := ollama.NewManager(cfg.Ollama)
		if err := mgr.Stop(); err != nil {
			return err
		}
		fmt.Println(cCyan + "Ollama stopped." + cReset)
		return nil
	},
}

var ollamaPullCmd = &cobra.Command{
	Use:   "pull [model]",
	Short: "Pull an Ollama model",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.Load()
		if cfg == nil {
			cfg = config.DefaultConfig()
		}
		mgr := ollama.NewManager(cfg.Ollama)
		model := args[0]
		fmt.Printf(cDim+"Pulling %s..."+cReset+"\n", model)
		if err := mgr.Pull(context.Background(), model); err != nil {
			return err
		}
		fmt.Printf(cGreen+"Model %s is ready!"+cReset+"\n", model)
		return nil
	},
}

var ollamaListCmd = &cobra.Command{
	Use:   "list",
	Short: "List local Ollama models",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.Load()
		if cfg == nil {
			cfg = config.DefaultConfig()
		}
		mgr := ollama.NewManager(cfg.Ollama)
		models, err := mgr.ListModels(context.Background())
		if err != nil {
			return fmt.Errorf("could not list models: %w", err)
		}
		if len(models) == 0 {
			fmt.Println("No models found.")
			fmt.Println(cDim + "Pull one with: " + cReset + "minikrill ollama pull llama3.2")
			return nil
		}
		fmt.Println(cBold + "Local models:" + cReset)
		for _, m := range models {
			fmt.Printf("  "+cCyan+"%-25s"+cReset+cDim+" %.1f GB\n"+cReset, m.Name, float64(m.Size)/(1024*1024*1024))
		}
		return nil
	},
}

var ollamaStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check Ollama status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.Load()
		if cfg == nil {
			cfg = config.DefaultConfig()
		}
		mgr := ollama.NewManager(cfg.Ollama)
		status := mgr.Status(context.Background())
		switch status {
		case "running":
			fmt.Println(cGreen + "Ollama: running" + cReset)
		case "stopped":
			fmt.Println(cYellow + "Ollama: stopped" + cReset)
			fmt.Println(cDim + "Start with: " + cReset + "minikrill ollama start")
		default:
			fmt.Println(cRed + "Ollama: " + status + cReset)
		}
		return nil
	},
}

// ---------------------------------------------------------------------------
// skill subcommands
// ---------------------------------------------------------------------------

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage skills",
}

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available skills",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.Load()
		if cfg != nil {
			_ = klog.InitQuiet(cfg.Log)
		}
		reg := plugin.NewRegistry()
		reg.RegisterBuiltins()
		if cfg != nil && cfg.Plugins.Dir != "" {
			_ = reg.LoadSkillsFromDir(cfg.Plugins.Dir)
		}
		skills := reg.List()
		if len(skills) == 0 {
			fmt.Println("No skills registered.")
			return nil
		}
		fmt.Println(cBold + "Available skills:" + cReset)
		for _, s := range skills {
			status := cGreen + "on" + cReset
			if !s.Enabled {
				status = cDim + "off" + cReset
			}
			fmt.Printf("  [%s] "+cCyan+"%-15s"+cReset+" %s\n", status, s.Name, s.Description)
		}
		fmt.Println()
		fmt.Println(cDim + "Add custom skills as YAML files in: " + cReset + config.DataDir() + "/skills/")
		return nil
	},
}

// ---------------------------------------------------------------------------
// brain subcommands
// ---------------------------------------------------------------------------

var brainCmd = &cobra.Command{
	Use:   "brain",
	Short: "Inspect and manage the krill's brain",
}

var brainStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show brain status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		_ = klog.InitQuiet(cfg.Log)
		krillBrain, err := brain.New(cfg.Brain, nil)
		if err != nil {
			return fmt.Errorf("init brain: %w", err)
		}
		p := krillBrain.GetPersonality()
		fmt.Println()
		fmt.Printf(cDim+"  Personality: "+cReset+cBCyan+"%s\n"+cReset, p.Name)
		fmt.Printf(cDim+"  Traits:      "+cReset+"%s\n", strings.Join(p.Traits, ", "))
		fmt.Printf(cDim+"  Style:       "+cReset+"%s\n", p.Style)
		fmt.Printf(cDim+"  Memories:    "+cReset+"%d\n", krillBrain.Memory().Count())
		fmt.Printf(cDim+"  Data dir:    "+cReset+"%s\n", cfg.Brain.DataDir)
		fmt.Println()
		return nil
	},
}

var brainRecallCmd = &cobra.Command{
	Use:   "recall [key]",
	Short: "Recall a memory by key",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		_ = klog.InitQuiet(cfg.Log)
		krillBrain, err := brain.New(cfg.Brain, nil)
		if err != nil {
			return err
		}
		entry, err := krillBrain.Memory().Recall(context.Background(), args[0])
		if err != nil {
			return fmt.Errorf("memory not found: %s", args[0])
		}
		fmt.Printf(cDim+"Key:     "+cReset+cCyan+"%s\n"+cReset, entry.Key)
		fmt.Printf(cDim+"Value:   "+cReset+"%s\n", entry.Value)
		fmt.Printf(cDim+"Tags:    "+cReset+"%s\n", strings.Join(entry.Tags, ", "))
		fmt.Printf(cDim+"Created: "+cReset+"%s\n", entry.CreatedAt.Format("2006-01-02 15:04:05"))
		return nil
	},
}

var brainForgetCmd = &cobra.Command{
	Use:   "forget [key]",
	Short: "Forget a memory",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		_ = klog.InitQuiet(cfg.Log)
		krillBrain, err := brain.New(cfg.Brain, nil)
		if err != nil {
			return err
		}
		if err := krillBrain.Memory().Forget(context.Background(), args[0]); err != nil {
			return err
		}
		fmt.Printf(cDim+"Forgot: "+cReset+"%s\n", args[0])
		return nil
	},
}

var brainSearchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search memories",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		_ = klog.InitQuiet(cfg.Log)
		krillBrain, err := brain.New(cfg.Brain, nil)
		if err != nil {
			return err
		}
		entries, err := krillBrain.Memory().Search(context.Background(), args[0], 10)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			fmt.Println("No memories found.")
			return nil
		}
		for _, e := range entries {
			fmt.Printf("  "+cCyan+"[%s]"+cReset+" %s\n", e.Key, truncate(e.Value, 80))
		}
		return nil
	},
}

// ---------------------------------------------------------------------------
// personality subcommands
// ---------------------------------------------------------------------------

var personalityCmd = &cobra.Command{
	Use:   "personality",
	Short: "Manage krill personalities",
}

var personalityListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available personalities",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.Load()
		active := "krill"
		if cfg != nil && cfg.Agent.Personality != "" {
			active = cfg.Agent.Personality
		}
		names := brain.ListPersonalities(config.DataDir())
		fmt.Println(cBold + "Personalities:" + cReset)
		for _, n := range names {
			marker := "  "
			if n == active {
				marker = cBGreen + "> " + cReset
			}
			fmt.Printf("  %s"+cCyan+"%s"+cReset+"\n", marker, n)
		}
		fmt.Println()
		fmt.Println(cDim + "Create new: " + cReset + "minikrill personality create")
		fmt.Println(cDim + "Switch:     " + cReset + "minikrill personality switch <name>")
		return nil
	},
}

var personalityCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new personality (interactive)",
	RunE: func(cmd *cobra.Command, args []string) error {
		scanner := bufio.NewScanner(os.Stdin)

		fmt.Println()
		fmt.Println(cBCyan + "  Create a new Mini Krill personality" + cReset)
		fmt.Println(cDim + "  Give your krill a unique identity." + cReset)
		fmt.Println()

		name := ask(scanner, cCyan+"  Name (lowercase, no spaces): "+cReset)
		if name == "" {
			return fmt.Errorf("name is required")
		}
		name = strings.ToLower(strings.ReplaceAll(name, " ", "-"))

		displayName := ask(scanner, cCyan+"  Display name: "+cReset)
		if displayName == "" {
			displayName = strings.Title(name)
		}

		style := ask(scanner, cCyan+"  Style (e.g. 'formal and precise', 'casual and fun'): "+cReset)
		if style == "" {
			style = "Friendly and helpful"
		}

		traitsStr := ask(scanner, cCyan+"  Traits (comma-separated, e.g. 'witty, bold, creative'): "+cReset)
		traits := []string{}
		for _, t := range strings.Split(traitsStr, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				traits = append(traits, t)
			}
		}
		if len(traits) == 0 {
			traits = []string{"helpful", "friendly"}
		}

		greeting := ask(scanner, cCyan+"  Greeting message: "+cReset)
		if greeting == "" {
			greeting = fmt.Sprintf("Hey! %s here, ready to help.", displayName)
		}

		identity := ask(scanner, cCyan+"  Identity (one line about who this personality is): "+cReset)
		if identity == "" {
			identity = fmt.Sprintf("%s - a custom Mini Krill personality", displayName)
		}

		systemPrompt := ask(scanner, cCyan+"  Custom system prompt "+cDim+"(press Enter for default): "+cReset)

		// Build personality
		p := &core.Personality{
			Name:       displayName,
			Traits:     traits,
			Style:      style,
			Quirks:     []string{},
			KrillFacts: core.KrillFacts,
			Greeting:   greeting,
		}

		soul := &core.Soul{
			SystemPrompt: systemPrompt, // empty = will use default
			Identity:     identity,
			Values:       []string{"Be helpful", "Be honest", "Plan before acting"},
			Boundaries:   []string{"Never execute without showing a plan"},
		}

		if err := brain.SavePersonality(name, config.DataDir(), soul, p); err != nil {
			return err
		}

		fmt.Println()
		fmt.Printf(cBGreen+"  Personality '%s' created!"+cReset+"\n", name)
		fmt.Printf(cDim+"  Switch to it: "+cReset+"minikrill personality switch %s\n", name)
		fmt.Println()
		return nil
	},
}

var personalitySwitchCmd = &cobra.Command{
	Use:   "switch [name]",
	Short: "Switch active personality",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := strings.ToLower(args[0])

		// Verify it exists
		names := brain.ListPersonalities(config.DataDir())
		found := false
		for _, n := range names {
			if n == name {
				found = true
				break
			}
		}
		if !found {
			fmt.Printf(cRed+"Personality '%s' not found."+cReset+"\n", name)
			fmt.Println(cDim + "Available:" + cReset)
			for _, n := range names {
				fmt.Printf("  %s\n", n)
			}
			return nil
		}

		cfg, err := config.Load()
		if err != nil {
			return err
		}
		cfg.Agent.Personality = name
		if err := config.Save(cfg); err != nil {
			return err
		}

		fmt.Printf(cBGreen+"Switched to personality: %s"+cReset+"\n", name)
		fmt.Println(cDim + "Restart chat/dive for it to take effect." + cReset)
		return nil
	},
}

var personalityShowCmd = &cobra.Command{
	Use:   "show [name]",
	Short: "Show personality details",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.Load()
		name := "krill"
		if cfg != nil && cfg.Agent.Personality != "" {
			name = cfg.Agent.Personality
		}
		if len(args) > 0 {
			name = args[0]
		}

		_ = klog.InitQuiet(config.LogConfig{})
		soul, p, err := brain.LoadPersonalityByName(name, config.DataDir(), "")
		if err != nil {
			return err
		}

		fmt.Println()
		fmt.Printf(cDim+"  Name:     "+cReset+cBCyan+"%s"+cReset+"\n", p.Name)
		fmt.Printf(cDim+"  Traits:   "+cReset+"%s\n", strings.Join(p.Traits, ", "))
		fmt.Printf(cDim+"  Style:    "+cReset+"%s\n", p.Style)
		fmt.Printf(cDim+"  Identity: "+cReset+"%s\n", soul.Identity)
		fmt.Printf(cDim+"  Greeting: "+cReset+"%s\n", p.Greeting)
		if len(p.Quirks) > 0 {
			fmt.Printf(cDim+"  Quirks:   "+cReset+"%s\n", strings.Join(p.Quirks, "; "))
		}
		fmt.Println()
		return nil
	},
}

// renderMarkdown converts common markdown patterns to ANSI-colored terminal output.
// Handles: bold, italic, inline code, code blocks, headers, lists, and links.
func renderMarkdown(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	inCodeBlock := false

	for _, line := range lines {
		// Code blocks (``` ... ```)
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCodeBlock = !inCodeBlock
			if inCodeBlock {
				out = append(out, cDim+"  "+strings.Repeat("-", 40)+cReset)
			} else {
				out = append(out, cDim+"  "+strings.Repeat("-", 40)+cReset)
			}
			continue
		}
		if inCodeBlock {
			out = append(out, cDimCyan+"  "+line+cReset)
			continue
		}

		// Headers: # ## ###
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### ") {
			out = append(out, cBCyan+strings.TrimPrefix(trimmed, "### ")+cReset)
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			out = append(out, cBCyan+strings.TrimPrefix(trimmed, "## ")+cReset)
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			out = append(out, cBold+cCyan+strings.TrimPrefix(trimmed, "# ")+cReset)
			continue
		}

		// Bullet lists: - or *
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			bullet := cCyan + "  -" + cReset
			content := renderInline(trimmed[2:])
			out = append(out, bullet+" "+content)
			continue
		}

		// Numbered lists: 1. 2. etc.
		if len(trimmed) > 2 && trimmed[0] >= '0' && trimmed[0] <= '9' && strings.Contains(trimmed[:3], ".") {
			dotIdx := strings.Index(trimmed, ".")
			if dotIdx > 0 && dotIdx < 4 {
				num := trimmed[:dotIdx]
				rest := strings.TrimSpace(trimmed[dotIdx+1:])
				out = append(out, cCyan+"  "+num+"."+cReset+" "+renderInline(rest))
				continue
			}
		}

		// Regular line - apply inline formatting
		out = append(out, renderInline(line))
	}

	return strings.Join(out, "\n")
}

// renderInline handles inline markdown: **bold**, *italic*, `code`, [links](url)
func renderInline(s string) string {
	// Bold: **text**
	for {
		start := strings.Index(s, "**")
		if start == -1 {
			break
		}
		end := strings.Index(s[start+2:], "**")
		if end == -1 {
			break
		}
		end += start + 2
		inner := s[start+2 : end]
		s = s[:start] + cBold + inner + cReset + s[end+2:]
	}

	// Italic: *text* (but not **)
	for {
		start := -1
		for i := 0; i < len(s); i++ {
			if s[i] == '*' && (i+1 >= len(s) || s[i+1] != '*') && (i == 0 || s[i-1] != '*') {
				start = i
				break
			}
		}
		if start == -1 {
			break
		}
		end := -1
		for i := start + 1; i < len(s); i++ {
			if s[i] == '*' && (i+1 >= len(s) || s[i+1] != '*') && s[i-1] != '*' {
				end = i
				break
			}
		}
		if end == -1 {
			break
		}
		inner := s[start+1 : end]
		s = s[:start] + cDim + inner + cReset + s[end+1:]
	}

	// Inline code: `text`
	for {
		start := strings.Index(s, "`")
		if start == -1 {
			break
		}
		end := strings.Index(s[start+1:], "`")
		if end == -1 {
			break
		}
		end += start + 1
		inner := s[start+1 : end]
		s = s[:start] + cDimCyan + inner + cReset + s[end+1:]
	}

	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
