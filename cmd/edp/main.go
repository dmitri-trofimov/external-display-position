//go:build windows

package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"external-display-position/internal/config"
	"external-display-position/internal/display"
	"external-display-position/internal/watcher"
)

func main() {
	pause := hasPauseFlag(os.Args[1:])
	err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}
	if pause {
		fmt.Print("\nPress Enter to close...")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	if err != nil {
		os.Exit(1)
	}
}

func hasPauseFlag(args []string) bool {
	for _, a := range args {
		if a == "--pause" || a == "-p" {
			return true
		}
	}
	return false
}

func stripPauseFlag(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a != "--pause" && a != "-p" {
			out = append(out, a)
		}
	}
	return out
}

func run(args []string) error {
	args = stripPauseFlag(args)
	fs := flag.NewFlagSet("edp", flag.ContinueOnError)
	fs.Usage = printUsage
	cfgFlag := fs.String("config", "", "path to config.json (default: %LOCALAPPDATA%\\ExternalDisplayPosition\\config.json)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	rest := fs.Args()
	if len(rest) == 0 {
		printUsage()
		return nil
	}

	configPath := *cfgFlag
	isCustomConfig := configPath != ""
	if configPath == "" {
		configPath = config.DefaultPath()
	}

	cmd := strings.ToLower(rest[0])
	cmdArgs := rest[1:]

	switch cmd {
	case "status":
		return cmdStatus(configPath)
	case "save":
		name := ""
		if len(cmdArgs) > 0 {
			name = strings.Join(cmdArgs, " ")
		}
		return cmdSave(configPath, name)
	case "list":
		return cmdList(configPath)
	case "apply":
		return cmdApply(configPath)
	case "delete", "rm":
		if len(cmdArgs) == 0 {
			return fmt.Errorf("usage: edp delete <name-or-signature>")
		}
		return cmdDelete(configPath, strings.Join(cmdArgs, " "))
	case "watch":
		release, err := watcher.AcquireSingleInstance()
		if err != nil {
			return err
		}
		defer release()
		return watcher.Run(configPath, os.Stdout)
	case "autostart":
		return handleAutostart(cmdArgs, configPath, isCustomConfig)
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func cmdStatus(configPath string) error {
	snap, err := display.QueryActive()
	if err != nil {
		return err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	watcherState := "NOT running (start edpw.exe)"
	if watcher.IsRunning() {
		watcherState = "running"
	}

	fmt.Printf("Watcher     : %s\n", watcherState)
	fmt.Printf("Config file : %s\n", configPath)
	fmt.Printf("Signature   : %s\n", snap.Signature)
	fmt.Println("Active displays:")
	for _, d := range snap.Displays {
		name := d.FriendlyName
		if name == "" {
			name = "(unnamed)"
		}
		fmt.Printf("  %-18s %-28s %dx%d at (%d, %d)\n", d.ID, name, d.Width, d.Height, d.X, d.Y)
	}

	prof, ok := cfg.Profiles[snap.Signature]
	if !ok {
		fmt.Println("Saved match : none (run 'edp save <name>' to save this layout)")
		return nil
	}

	inSync := true
	for _, d := range snap.Displays {
		want, exists := prof.Positions[d.ID]
		if !exists || want.X != d.X || want.Y != d.Y {
			inSync = false
			break
		}
	}

	label := prof.Name
	if label == "" {
		label = "(unnamed)"
	}
	state := "in sync"
	if !inSync {
		state = "differs from current positions"
	}
	fmt.Printf("Saved match : %s [%s]\n", label, state)
	return nil
}

func cmdSave(configPath, name string) error {
	snap, err := display.QueryActive()
	if err != nil {
		return err
	}
	if len(snap.Displays) == 0 {
		return fmt.Errorf("no active displays found")
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	prof := cfg.UpsertSnapshot(name, snap)
	if err := config.Save(configPath, cfg); err != nil {
		return err
	}

	label := prof.Name
	if label == "" {
		label = "(unnamed)"
	}
	fmt.Printf("Saved profile %s\n", label)
	fmt.Printf("  Signature: %s\n", snap.Signature)
	for _, d := range snap.Displays {
		fmt.Printf("  %-18s -> (%d, %d)\n", d.ID, d.X, d.Y)
	}
	fmt.Printf("  Config   : %s\n", configPath)
	return nil
}

func cmdList(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	fmt.Printf("Config: %s\n", configPath)
	if len(cfg.Profiles) == 0 {
		fmt.Println("No saved profiles yet.")
		return nil
	}

	sigs := make([]string, 0, len(cfg.Profiles))
	for sig := range cfg.Profiles {
		sigs = append(sigs, sig)
	}
	sort.Strings(sigs)

	for _, sig := range sigs {
		prof := cfg.Profiles[sig]
		label := prof.Name
		if label == "" {
			label = "(unnamed)"
		}
		fmt.Printf("\n* %s\n", label)
		fmt.Printf("  Signature: %s\n", sig)
		fmt.Printf("  Updated  : %s\n", prof.UpdatedAt.Format("2006-01-02 15:04:05"))

		ids := make([]string, 0, len(prof.Positions))
		for id := range prof.Positions {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			pos := prof.Positions[id]
			fmt.Printf("  %-18s -> (%d, %d)\n", id, pos.X, pos.Y)
		}
	}
	return nil
}

func cmdApply(configPath string) error {
	snap, err := display.QueryActive()
	if err != nil {
		return err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	prof, ok := cfg.Profiles[snap.Signature]
	if !ok {
		return fmt.Errorf("no saved profile matches active signature %q", snap.Signature)
	}

	changed, err := snap.ApplyPositions(prof.Positions)
	if err != nil {
		return err
	}
	if !changed {
		fmt.Println("Already matching saved layout.")
		return nil
	}
	fmt.Println("Applied saved layout.")
	return nil
}

func cmdDelete(configPath, key string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	sig, ok := cfg.Delete(key)
	if !ok {
		return fmt.Errorf("no saved profile found matching %q", key)
	}
	if err := config.Save(configPath, cfg); err != nil {
		return err
	}
	fmt.Printf("Deleted profile %s\n", sig)
	return nil
}

func printUsage() {
	fmt.Println(`edp - External Display Position manager

Usage:
  edp [--config <path>] [--pause] <command>

Commands:
  status                    Show active displays, port UIDs, and matching profile
  save [name]               Save current display positions for this port topology
  list                      List all saved profiles
  apply                     Apply the saved profile for the current topology
  delete <name|signature>   Delete a saved profile
  watch                     Run display watcher in foreground (for testing)
  autostart <action>        Manage Windows logon autostart (enable|disable|status)

Flags:
  --pause, -p               Wait for Enter before exiting (used by Desktop shortcut)`)
}
