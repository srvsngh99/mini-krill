package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/srvsngh99/mini-krill/internal/brain"
	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
	klog "github.com/srvsngh99/mini-krill/internal/log"
)

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
		var traits []string
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

		p := &core.Personality{
			Name:       displayName,
			Traits:     traits,
			Style:      style,
			Quirks:     []string{},
			KrillFacts: core.KrillFacts,
			Greeting:   greeting,
		}

		soul := &core.Soul{
			SystemPrompt: systemPrompt,
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
