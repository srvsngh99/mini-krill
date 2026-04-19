package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

var ollamaCmd = &cobra.Command{
	Use:   "ollama",
	Short: "Manage local Ollama installation",
}

var ollamaInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Ollama",
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, _ := newOllamaManager()
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
		mgr, _ := newOllamaManager()
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
		mgr, _ := newOllamaManager()
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
		mgr, _ := newOllamaManager()
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
		mgr, _ := newOllamaManager()
		models, err := mgr.ListModels(context.Background())
		if err != nil {
			return fmt.Errorf("could not list models: %w", err)
		}
		if len(models) == 0 {
			fmt.Println("No models found.")
			fmt.Println(cDim + "Pull one with: " + cReset + "minikrill ollama pull gemma4:e2b")
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
		mgr, _ := newOllamaManager()
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

var ollamaEnsureCmd = &cobra.Command{
	Use:   "ensure",
	Short: "Install, start, and pull the default model in one shot",
	Long:  "Ensures Ollama is installed, running, and has the default model ready. Other bots can call this at startup.",
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr, cfg := newOllamaManager()
		ctx := context.Background()

		// Install if missing
		if !mgr.IsInstalled() {
			fmt.Println(cDim + "Installing Ollama..." + cReset)
			if err := mgr.Install(ctx); err != nil {
				return fmt.Errorf("install failed: %w", err)
			}
			fmt.Println(cGreen + "Installed." + cReset)
		}

		// Start if stopped
		if err := mgr.EnsureRunning(ctx); err != nil {
			return fmt.Errorf("start failed: %w", err)
		}

		// Pull default model if not present
		model := cfg.Ollama.DefaultModel
		if model == "" {
			model = "gemma4:e2b"
		}
		if !mgr.HasModel(ctx, model) {
			fmt.Printf(cDim+"Pulling %s..."+cReset+"\n", model)
			if err := mgr.Pull(ctx, model); err != nil {
				return fmt.Errorf("pull failed: %w", err)
			}
		}

		fmt.Println("ready")
		return nil
	},
}
