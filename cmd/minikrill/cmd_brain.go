package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/srvsngh99/mini-krill/internal/brain"
)

var brainCmd = &cobra.Command{
	Use:   "brain",
	Short: "Inspect and manage the krill's brain",
}

var brainStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show brain status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfigWithLog()
		if err != nil {
			return err
		}
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
		cfg, err := loadConfigWithLog()
		if err != nil {
			return err
		}
		krillBrain, err := brain.New(cfg.Brain, nil)
		if err != nil {
			return err
		}
		entry, err := krillBrain.Memory().Recall(context.Background(), args[0])
		if err != nil {
			return fmt.Errorf("memory not found: %s", args[0])
		}
		if entry == nil {
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
		cfg, err := loadConfigWithLog()
		if err != nil {
			return err
		}
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
		cfg, err := loadConfigWithLog()
		if err != nil {
			return err
		}
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
			fmt.Printf("  "+cCyan+"[%s]"+cReset+" %s\n", e.Key, truncateStr(e.Value, 80))
		}
		return nil
	},
}
