package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/reminder"
)

var remindAt string

var remindCmd = &cobra.Command{
	Use:   "remind [text]",
	Short: "Create a durable local reminder",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := newReminderStore()
		if err != nil {
			return err
		}
		text, due, err := reminder.Parse(strings.Join(args, " "), remindAt, time.Now())
		if err != nil {
			return err
		}
		r, err := store.Add(text, due)
		if err != nil {
			return err
		}
		fmt.Printf("Reminder %s set for %s: %s\n", r.ID, r.DueAt.Local().Format("2006-01-02 15:04"), r.Text)
		return nil
	},
}

var remindersCmd = &cobra.Command{
	Use:   "reminders",
	Short: "List and manage reminders",
}

var remindersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List active reminders",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := newReminderStore()
		if err != nil {
			return err
		}
		items, err := store.List()
		if err != nil {
			return err
		}
		if len(items) == 0 {
			fmt.Println("No reminders.")
			return nil
		}
		for _, r := range items {
			status := "pending"
			if r.DoneAt != nil {
				status = "done"
			} else if r.FiredAt != nil {
				status = "fired"
			} else if !r.DueAt.After(time.Now()) {
				status = "due"
			}
			fmt.Printf("%s  %-7s  %s  %s\n", r.ID, status, r.DueAt.Local().Format("2006-01-02 15:04"), r.Text)
		}
		return nil
	},
}

var remindersDueCmd = &cobra.Command{
	Use:   "due",
	Short: "Show due reminders",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := newReminderStore()
		if err != nil {
			return err
		}
		items, err := store.Due(time.Now())
		if err != nil {
			return err
		}
		if len(items) == 0 {
			fmt.Println("No due reminders.")
			return nil
		}
		for _, r := range items {
			fmt.Printf("%s  %s\n", r.ID, r.Text)
		}
		return nil
	},
}

var remindersDoneCmd = &cobra.Command{
	Use:   "done [id]",
	Short: "Mark a reminder done",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := newReminderStore()
		if err != nil {
			return err
		}
		if err := store.MarkDone(args[0]); err != nil {
			return err
		}
		fmt.Println("done")
		return nil
	},
}

var remindersDeleteCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete a reminder",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := newReminderStore()
		if err != nil {
			return err
		}
		if err := store.Delete(args[0]); err != nil {
			return err
		}
		fmt.Println("deleted")
		return nil
	},
}

func newReminderStore() (*reminder.Store, error) {
	return reminder.NewStore(filepath.Join(config.DataDir(), "reminders.jsonl"))
}

func init() {
	remindCmd.Flags().StringVar(&remindAt, "at", "", "due time as duration, RFC3339, or 'YYYY-MM-DD HH:MM'")
	remindersCmd.AddCommand(remindersListCmd, remindersDueCmd, remindersDoneCmd, remindersDeleteCmd)
}
