package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
	"github.com/srvsngh99/mini-krill/internal/reminder"
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
		fmt.Println(cBCyan + "  Required:" + cReset + " choose one model provider.")
		fmt.Println(cBCyan + "  Optional:" + cReset + " Telegram/Discord bots, Codex login, Claude login.")
		fmt.Println(cDim + "  Recommended local model: gemma3:4b. Low-memory fallback: llama3.2:3b." + cReset)
		fmt.Println()

		// Load existing config so tokens and other settings survive re-init.
		cfg, _ := config.Load()
		if cfg == nil {
			cfg = config.DefaultConfig()
		}
		scanner := bufio.NewScanner(os.Stdin)

		fmt.Println(cBCyan + "  Choose your LLM provider:" + cReset)
		fmt.Println()
		providerIdx, err := promptSelect([]selectItem{
			{label: "Ollama", desc: "local, free, private; pulls gemma3:4b"},
			{label: "Codex", desc: "ChatGPT subscription via official Codex CLI"},
			{label: "Claude Code", desc: "Claude Pro/Max via official Claude CLI"},
		})
		if err != nil {
			return err
		}
		if providerIdx < 0 {
			fmt.Println(cDim + "  Setup cancelled." + cReset)
			return nil
		}

		switch providerIdx {
		case 0: // Ollama
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
			// Build model list: locally available models first, then suggestions, then Custom.
			var modelItems []selectItem
			var modelNames []string
			localSet := make(map[string]bool)

			if models, err := mgr.ListModels(context.Background()); err == nil && len(models) > 0 {
				for _, m := range models {
					modelItems = append(modelItems, selectItem{label: m.Name, desc: "already downloaded"})
					modelNames = append(modelNames, m.Name)
					localSet[m.Name] = true
				}
			}

			suggested := []struct{ name, desc string }{
				{"gemma3:4b", "recommended; balanced speed and quality"},
				{"llama3.2:3b", "low-memory fallback"},
				{"mistral:7b", "strong general-purpose model"},
			}
			for _, s := range suggested {
				if !localSet[s.name] {
					modelItems = append(modelItems, selectItem{label: s.name, desc: s.desc + " (will download)"})
					modelNames = append(modelNames, s.name)
				}
			}
			modelItems = append(modelItems, selectItem{label: "Custom...", desc: "enter a model name manually"})
			modelNames = append(modelNames, "")

			fmt.Println(cBCyan + "  Choose a model:" + cReset)
			fmt.Println()
			modelIdx, err := promptSelect(modelItems)
			if err != nil {
				return err
			}
			if modelIdx < 0 {
				fmt.Println(cDim + "  Setup cancelled." + cReset)
				return nil
			}

			selectedModel := modelNames[modelIdx]
			if selectedModel == "" { // Custom
				selectedModel = ask(scanner, cCyan+"  Model name: "+cReset)
				selectedModel = strings.TrimSpace(selectedModel)
				if selectedModel == "" {
					selectedModel = cfg.Ollama.DefaultModel
				}
			}
			cfg.LLM.Model = selectedModel
			cfg.Ollama.DefaultModel = selectedModel

			if !localSet[selectedModel] {
				fmt.Println()
				pullCtx, pullStop := signal.NotifyContext(context.Background(), os.Interrupt)
				defer pullStop()
				if err := mgr.EnsureRunning(pullCtx); err != nil {
					fmt.Printf(cYellow+"  Could not start Ollama: %v\n"+cReset, err)
					fmt.Println(cDim + "  You can pull the model later with: ollama pull " + selectedModel + cReset)
				} else if err := mgr.PullWithProgress(pullCtx, selectedModel, os.Stdout); err != nil {
					if pullCtx.Err() != nil {
						fmt.Println(cDim + "\n  Download interrupted." + cReset)
					} else {
						fmt.Printf(cYellow+"  Pull failed: %v\n"+cReset, err)
					}
					fmt.Println(cDim + "  You can retry with: ollama pull " + selectedModel + cReset)
				}
				pullStop()
			}
		case 1: // Codex
			cfg.LLM.Provider = "codex"
			cfg.LLM.Model = "auto"
			fmt.Println()
			fmt.Println(cDim + "  Mini Krill stores no Codex OAuth tokens. The official Codex CLI owns login." + cReset)
			if _, err := exec.LookPath("codex"); err != nil {
				fmt.Println(cYellow + "  Codex CLI not found. Install it, then run: codex login" + cReset)
			} else {
				fmt.Println(cDim + "  Codex CLI found." + cReset)
				ans := ask(scanner, cCyan+"  Run Codex login now? "+cDim+"[Y/n]"+cReset+" ")
				if ans == "" || strings.HasPrefix(strings.ToLower(ans), "y") {
					cmd := exec.Command("codex", "login")
					cmd.Stdin = os.Stdin
					cmd.Stdout = os.Stdout
					cmd.Stderr = os.Stderr
					_ = cmd.Run()
				}
			}
		case 2: // Claude Code
			cfg.LLM.Provider = "claude"
			cfg.LLM.Model = "auto"
			fmt.Println()
			fmt.Println(cDim + "  Mini Krill stores no Claude OAuth tokens. The official Claude CLI owns login." + cReset)
			if _, err := exec.LookPath("claude"); err != nil {
				fmt.Println(cYellow + "  Claude CLI not found. Install Claude Code, then run: claude auth login" + cReset)
			} else {
				fmt.Println(cDim + "  Claude CLI found." + cReset)
				ans := ask(scanner, cCyan+"  Run Claude login now? "+cDim+"[Y/n]"+cReset+" ")
				if ans == "" || strings.HasPrefix(strings.ToLower(ans), "y") {
					cmd := exec.Command("claude", "auth", "login")
					cmd.Stdin = os.Stdin
					cmd.Stdout = os.Stdout
					cmd.Stderr = os.Stderr
					_ = cmd.Run()
				}
			}
		default:
			return fmt.Errorf("unexpected provider index: %d", providerIdx)
		}

		fmt.Println()
		// Telegram setup - preserve existing token and enabled state if user skips
		if cfg.Telegram.Token != "" {
			fmt.Println(cDim + "  Telegram bot token already configured." + cReset)
			if ans := ask(scanner, cCyan+"  Replace Telegram token? "+cDim+"[y/N]"+cReset+" "); strings.HasPrefix(strings.ToLower(ans), "y") {
				fmt.Println(cDim + "  To get a bot token, message @BotFather on Telegram → /newbot → follow the prompts." + cReset)
				token := ask(scanner, cCyan+"  Bot token: "+cReset)
				if token != "" {
					cfg.Telegram.Token = token
					cfg.Telegram.Enabled = true
				}
			}
		} else if ans := ask(scanner, cCyan+"  Enable Telegram bot? "+cDim+"[y/N]"+cReset+" "); strings.HasPrefix(strings.ToLower(ans), "y") {
			fmt.Println(cDim + "  To get a bot token, message @BotFather on Telegram → /newbot → follow the prompts." + cReset)
			cfg.Telegram.Token = ask(scanner, cCyan+"  Bot token: "+cReset)
			cfg.Telegram.Enabled = cfg.Telegram.Token != ""
		}
		if cfg.Telegram.Enabled {
			fmt.Println(cDim + "  Telegram bot saved! Run " + cReset + "minikrill dive" + cDim + " to start it, then send " + cReset + "/start" + cDim + " to your bot on Telegram." + cReset)
		}

		// Discord setup - preserve existing token and enabled state if user skips
		if cfg.Discord.Token != "" {
			fmt.Println(cDim + "  Discord bot token already configured." + cReset)
			if ans := ask(scanner, cCyan+"  Replace Discord token? "+cDim+"[y/N]"+cReset+" "); strings.HasPrefix(strings.ToLower(ans), "y") {
				fmt.Println(cDim + "  Create a bot at https://discord.com/developers/applications → Bot → Copy token." + cReset)
				token := ask(scanner, cCyan+"  Bot token: "+cReset)
				if token != "" {
					cfg.Discord.Token = token
					cfg.Discord.Enabled = true
				}
			}
		} else if ans := ask(scanner, cCyan+"  Enable Discord bot? "+cDim+"[y/N]"+cReset+" "); strings.HasPrefix(strings.ToLower(ans), "y") {
			fmt.Println(cDim + "  Create a bot at https://discord.com/developers/applications → Bot → Copy token." + cReset)
			cfg.Discord.Token = ask(scanner, cCyan+"  Bot token: "+cReset)
			cfg.Discord.Enabled = cfg.Discord.Token != ""
		}
		if cfg.Discord.Enabled {
			fmt.Println(cDim + "  Discord bot saved! Run " + cReset + "minikrill dive" + cDim + " to start it." + cReset)
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
		fmt.Println(cDim + "  Install with " + cReset + "go install ./cmd/minikrill" + cDim + " to use " + cReset + "minikrill" + cDim + " directly," + cReset)
		fmt.Println(cDim + "  or run immediately with " + cReset + "go run ./cmd/minikrill <command>" + cDim + "." + cReset)
		fmt.Println()
		fmt.Println(cBold + "  Next steps:" + cReset)
		fmt.Println("    " + cCyan + "minikrill chat" + cReset + "     Start chatting")
		fmt.Println("    " + cCyan + "minikrill tui" + cReset + "      Terminal dashboard")
		fmt.Println("    " + cCyan + "minikrill doctor" + cReset + "   Health check")
		if cfg.Telegram.Enabled && cfg.Discord.Enabled {
			fmt.Println("    " + cCyan + "minikrill dive" + cReset + "     Start Telegram and Discord bots")
		} else if cfg.Telegram.Enabled {
			fmt.Println("    " + cCyan + "minikrill dive" + cReset + "     Start Telegram bot")
		} else if cfg.Discord.Enabled {
			fmt.Println("    " + cCyan + "minikrill dive" + cReset + "     Start Discord bot")
		}
		fmt.Println()
		return nil
	},
}

// ---------------------------------------------------------------------------
// dive command
// ---------------------------------------------------------------------------

var diveFG bool

func init() {
	diveCmd.Flags().BoolVarP(&diveFG, "foreground", "f", false, "run in foreground (default: daemonize)")
}

var diveCmd = &cobra.Command{
	Use:   "dive",
	Short: "Start Mini Krill - dive into the deep",
	Long:  "Starts the agent with all configured services (Telegram, Discord).",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !diveFG {
			return diveDaemon()
		}

		// Auto-start Ollama before initializing the stack
		var ollamaMgr *ollama.OllamaManager
		cfg, _ := config.Load()
		if cfg == nil {
			cfg = config.DefaultConfig()
		}
		if cfg.LLM.Provider == "ollama" && cfg.Ollama.AutoStart {
			ollamaMgr = ollama.NewManager(cfg.Ollama)
			if err := ollamaMgr.EnsureRunning(context.Background()); err != nil {
				klog.Warn("ollama auto-start failed (will retry via health monitor)", "error", err)
			}
		}

		stack, err := initStack(false)
		if err != nil {
			return err
		}
		defer stack.brain.Close()

		printBanner()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Start health monitor (does NOT stop Ollama on exit)
		if ollamaMgr != nil {
			ollamaMgr.StartHealthMonitor()
			defer ollamaMgr.StopHealthMonitor()
		}

		if err := stack.hb.Start(ctx); err != nil {
			klog.Warn("heartbeat start failed", "error", err)
		}

		fmt.Printf("  "+cDim+"Provider:"+cReset+" %s "+cDim+"|"+cReset+" %s\n", stack.cfg.LLM.Provider, stack.cfg.LLM.Model)
		fmt.Printf("  "+cDim+"Brain:"+cReset+"    %d memories\n", stack.brain.Memory().Count())
		fmt.Printf("  "+cDim+"Skills:"+cReset+"   %d registered\n", len(stack.skills.List()))

		bs := startBots(ctx, stack)
		if stack.cfg.Telegram.Enabled {
			if bs.TelegramOK {
				fmt.Println("  " + cGreen + "Telegram: swimming" + cReset)
			} else {
				fmt.Printf("  "+cRed+"Telegram: %s\n"+cReset, friendlyError(bs.TelegramErr))
			}
		}
		if stack.cfg.Discord.Enabled {
			if bs.DiscordOK {
				fmt.Println("  " + cGreen + "Discord:  swimming" + cReset)
			} else {
				fmt.Printf("  "+cRed+"Discord: %s\n"+cReset, friendlyError(bs.DiscordErr))
			}
		}
		if reminderStore, err := newReminderStore(); err == nil {
			go reminder.StartScheduler(ctx, reminderStore, time.Minute, func(r reminder.Reminder) {
				klog.Info("reminder due", "id", r.ID, "text", r.Text)
				fmt.Printf("\n%sReminder due:%s %s (%s)\n", cBYellow, cReset, r.Text, r.ID)
			})
			fmt.Println("  " + cGreen + "Reminders: watching" + cReset)
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
		signal.Notify(sigCh, terminationSignals()...)
		<-sigCh

		fmt.Println()
		fmt.Println(cCyan + "  Mini Krill is surfacing..." + cReset)
		cancel()
		_ = stack.hb.Stop()
		_ = stack.mcp.Close()
		return nil
	},
}

// diveDaemon re-execs the dive command as a detached background process.
// daemonRunning returns true if a dive daemon is already running.
func daemonRunning() bool {
	pidFile := filepath.Join(config.DataDir(), "krill.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return processRunning(proc)
}

func diveDaemon() error {
	// Check if a daemon is already running
	if daemonRunning() {
		pidFile := filepath.Join(config.DataDir(), "krill.pid")
		data, _ := os.ReadFile(pidFile)
		pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		return fmt.Errorf("Mini Krill is already diving (PID %d). Run 'minikrill surface' first", pid)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	logDir := filepath.Join(config.DataDir(), "logs")
	_ = os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, "dive.log")

	out, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	childArgs := []string{"dive", "--foreground"}
	if verbose {
		childArgs = append(childArgs, "--verbose")
	}

	child := exec.Command(exe, childArgs...)
	child.Stdout = out
	child.Stderr = out
	detachCommand(child)

	if err := child.Start(); err != nil {
		out.Close()
		return fmt.Errorf("start daemon: %w", err)
	}
	out.Close()

	// Detach - the parent does not wait for the child.
	_ = child.Process.Release()

	pidFile := filepath.Join(config.DataDir(), "krill.pid")
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0644)

	printBanner()
	fmt.Printf(cBGreen+"  Mini Krill is swimming!"+cReset+" "+cDim+"(PID: %d)\n"+cReset, child.Process.Pid)
	fmt.Printf(cDim+"  Log: %s\n"+cReset, logPath)
	fmt.Println()
	fmt.Println(cDim + "  Stop with: " + cReset + "minikrill surface")
	fmt.Println()
	return nil
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
		if err := terminateProcess(proc); err != nil {
			_ = proc.Kill()
		}
		_ = os.Remove(pidFile)
		fmt.Println(cCyan + "Mini Krill is surfacing..." + cReset)
		return nil
	},
}

// botStatus holds the result of starting chat bots.
type botStatus struct {
	TelegramOK  bool
	TelegramErr error
	DiscordOK   bool
	DiscordErr  error
}

// startBots launches Telegram and Discord bots in the background when enabled.
// Bots are tied to ctx and will shut down when the context is cancelled.
func startBots(ctx context.Context, stack *krillStack) botStatus {
	var s botStatus
	if stack.cfg.Telegram.Enabled {
		tgBot, err := chat.NewTelegramBot(stack.cfg.Telegram, stack.handler)
		if err != nil {
			s.TelegramErr = err
			klog.Warn("telegram bot failed to start", "error", err)
		} else {
			tgBot.SetProviderManager(stack.llm)
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
			s.TelegramOK = true
			klog.Info("telegram bot started")
		}
	}
	if stack.cfg.Discord.Enabled {
		dcBot, err := chat.NewDiscordBot(stack.cfg.Discord, stack.handler)
		if err != nil {
			s.DiscordErr = err
			klog.Warn("discord bot failed to start", "error", err)
		} else {
			go func() {
				if err := dcBot.Start(ctx); err != nil {
					klog.Error("discord error", "error", err)
				}
			}()
			s.DiscordOK = true
			klog.Info("discord bot started")
		}
	}
	return s
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
		defer stack.brain.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_ = stack.hb.Start(ctx)
		defer func() { _ = stack.hb.Stop() }()

		if !daemonRunning() {
			startBots(ctx, stack)
		}

		app := tui.NewApp(stack.agent, stack.brain, stack.hb, stack.skills, stack.mcp, core.Version, stack.cfg.Log.File)
		app.SetInitialTab(tui.TabChat)
		return app.Run()
	},
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
		defer stack.brain.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_ = stack.hb.Start(ctx)
		defer func() { _ = stack.hb.Stop() }()

		if !daemonRunning() {
			bs := startBots(ctx, stack)
			if stack.cfg.Telegram.Enabled && bs.TelegramErr != nil {
				klog.Error("telegram bot failed in TUI mode", "error", bs.TelegramErr)
			}
		}

		app := tui.NewApp(stack.agent, stack.brain, stack.hb, stack.skills, stack.mcp, core.Version, stack.cfg.Log.File)
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
