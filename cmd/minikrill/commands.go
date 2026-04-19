package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

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
// init command
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
		default:
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

		fmt.Println()
		if ans := ask(scanner, cCyan+"  Enable Telegram bot? "+cDim+"[y/N]"+cReset+" "); strings.HasPrefix(strings.ToLower(ans), "y") {
			cfg.Telegram.Token = ask(scanner, cCyan+"  Bot token: "+cReset)
			cfg.Telegram.Enabled = cfg.Telegram.Token != ""
		}
		if ans := ask(scanner, cCyan+"  Enable Discord bot? "+cDim+"[y/N]"+cReset+" "); strings.HasPrefix(strings.ToLower(ans), "y") {
			cfg.Discord.Token = ask(scanner, cCyan+"  Bot token: "+cReset)
			cfg.Discord.Enabled = cfg.Discord.Token != ""
		}

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
		fmt.Printf(cDimCyan+"  Fun fact: %s\n"+cReset, randomFact())
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
// dive command
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
		fmt.Println(cDimCyan + "  " + randomFact() + cReset)

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
// surface command
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
// chat command
// ---------------------------------------------------------------------------

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Interactive chat with Mini Krill",
	RunE: func(cmd *cobra.Command, args []string) error {
		stack, err := initStack(true)
		if err != nil {
			return err
		}

		printBanner()
		fmt.Printf(cDim+"  Provider: %s/%s"+cReset, stack.cfg.LLM.Provider, stack.cfg.LLM.Model)
		fmt.Printf(cDim+" | Brain: %d memories"+cReset, stack.brain.Memory().Count())
		fmt.Printf(cDim+" | Skills: %d"+cReset+"\n", len(stack.skills.List()))
		fmt.Println()

		greeting := stack.brain.GetPersonality().Greeting
		if greeting == "" {
			greeting = "Hey there! I'm Mini Krill, your crustaceous AI buddy."
		}
		fmt.Println(cBCyan + "  >=\\'>" + cReset + " " + greeting)
		fmt.Println()
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
				fmt.Printf(cBCyan+"krill > "+cReset+cDimCyan+"%s\n"+cReset, randomFact())
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

			done := make(chan struct{})
			go spinner(done)

			resp, err := stack.agent.Chat(ctx, input)
			close(done)

			fmt.Println()
			if err != nil {
				fmt.Printf(cBCyan+"krill > "+cReset+cYellow+"Bubbles! %s\n"+cReset, friendlyError(err))
				fmt.Println(cDim + "         Try rephrasing, or check 'minikrill doctor' for issues." + cReset)
			} else if resp == "" {
				fmt.Println(cBCyan + "krill > " + cReset + cDimCyan + randomFact() + cReset)
			} else {
				fmt.Println(cBCyan + "krill > " + cReset + renderMarkdown(resp))
			}
			fmt.Println()
		}
		return nil
	},
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

// ---------------------------------------------------------------------------
// tui command
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
		defer func() { _ = stack.hb.Stop() }()

		app := tui.NewApp(stack.agent, stack.brain, stack.hb, core.Version, stack.cfg.Log.File)
		return app.Run()
	},
}

// ---------------------------------------------------------------------------
// doctor command
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
// sonar command
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
		fmt.Println(cDimCyan + randomFact() + cReset)
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

