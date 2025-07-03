package main

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/evisdrenova/devgru/internal/config"
	"github.com/evisdrenova/devgru/internal/ide"
	"github.com/evisdrenova/devgru/internal/runner"
	"github.com/evisdrenova/devgru/ui"
)

func main() {
	if len(os.Args) == 1 {
		runInteractiveMode()
		return
	}
}

// runInteractiveMode starts the interactive TUI mode with auto IDE server
func runInteractiveMode() {
	cfg, err := config.LoadDefault()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		fmt.Fprintf(os.Stderr, "Make sure you have a devgru.yaml file in the current directory or ~/.devgru/\n")
		os.Exit(1)
	}

	r, err := runner.NewRunner(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create runner: %v\n", err)
		os.Exit(1)
	}
	defer r.Close()

	var ideServer *ide.Server

	// generates a unique port for the workspace so we can support multiple windows
	workspacePort := generateWorkspacePort()

	ideConfig := ide.Config{
		Enable:    true,
		Transport: cfg.IDE.Transport,
		DiffTool:  cfg.IDE.DiffTool,
		Port:      workspacePort,
	}

	ideServer = ide.NewServer(ideConfig)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := ideServer.Start(ctx); err != nil {
			// Don't exit on IDE server error, just log it
			fmt.Printf("IDE server warning: %v\n", err)
		}
	}()

	model := ui.NewInteractiveModel(r, cfg, ideServer)

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running interactive mode: %v\n", err)
		os.Exit(1)
	}
}

// Create a hash of the workspace path to generate a consistent port
// This ensures the same workspace always gets the same port
// Port range: 8123-8200 (77 possible ports)
func generateWorkspacePort() int {
	cwd, err := os.Getwd()
	if err != nil {
		return 8123
	}

	hash := simpleHash(cwd)
	port := 8123 + (hash % 77)

	return port
}

func simpleHash(s string) int {
	hash := 0
	// same logic as extension when it calcs a port
	for _, c := range s {
		hash = (hash*31 + int(c)) % 2147483647
	}
	if hash < 0 {
		hash = -hash
	}
	return hash
}
