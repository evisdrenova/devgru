package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/evisdrenova/devgru/internal/config"
	"github.com/evisdrenova/devgru/internal/ide"
)

func ideCommand(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: devgru ide <subcommand>\n")
		fmt.Fprintf(os.Stderr, "\nSubcommands:\n")
		fmt.Fprintf(os.Stderr, "  connect    Start IDE integration server\n")
		fmt.Fprintf(os.Stderr, "  status     Show IDE integration status\n")
		os.Exit(1)
	}

	subcommand := args[0]

	switch subcommand {
	case "connect":
		ideConnectCommand()
	case "status":
		ideStatusCommand()
	default:
		fmt.Fprintf(os.Stderr, "Unknown IDE subcommand: %s\n", subcommand)
		os.Exit(1)
	}
}

func ideConnectCommand() {
	// Load configuration
	cfg, err := config.LoadDefault()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		fmt.Fprintf(os.Stderr, "Make sure you have a devgru.yaml file in the current directory or ~/.devgru/\n")
		os.Exit(1)
	}

	if !cfg.IDE.Enable {
		fmt.Fprintf(os.Stderr, "IDE integration is disabled in config. Set ide.enable: true in devgru.yaml\n")
		os.Exit(1)
	}

	// Create IDE server
	ideConfig := ide.Config{
		Enable:    cfg.IDE.Enable,
		Transport: cfg.IDE.Transport,
		DiffTool:  cfg.IDE.DiffTool,
		Port:      cfg.IDE.Port,
	}

	server := ide.NewServer(ideConfig)

	// Set up graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nShutting down IDE server...")
		cancel()
	}()

	// Start the server
	fmt.Printf("Starting DevGru IDE integration...\n")
	fmt.Printf("🔌 VS Code extension should auto-detect and connect\n")
	fmt.Printf("📝 Open files and make selections in VS Code to provide context\n")
	fmt.Printf("⚡ Use Cmd+Esc (Mac) or Ctrl+Esc (Windows/Linux) to trigger DevGru\n")
	fmt.Printf("🛑 Press Ctrl+C to stop\n\n")

	if err := server.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "IDE server error: %v\n", err)
		os.Exit(1)
	}
}

func ideStatusCommand() {
	// Load configuration
	cfg, err := config.LoadDefault()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("DevGru IDE Integration Status\n")
	fmt.Printf("============================\n\n")

	if cfg.IDE.Enable {
		fmt.Printf("✅ IDE Integration: Enabled\n")
		fmt.Printf("🚀 Transport: %s\n", cfg.IDE.Transport)
		fmt.Printf("🔧 Diff Tool: %s\n", cfg.IDE.DiffTool)
		fmt.Printf("🌐 Port: %d\n", cfg.IDE.Port)
		fmt.Printf("\n")
		fmt.Printf("To connect:\n")
		fmt.Printf("  devgru ide connect\n")
		fmt.Printf("\n")
		fmt.Printf("VS Code Extension:\n")
		fmt.Printf("  Install: code --install-extension devgru.devgru-code\n")
		fmt.Printf("  Quick Launch: Cmd+Esc (Mac) / Ctrl+Esc (Windows/Linux)\n")
	} else {
		fmt.Printf("❌ IDE Integration: Disabled\n")
		fmt.Printf("\n")
		fmt.Printf("To enable, add to devgru.yaml:\n")
		fmt.Printf("  ide:\n")
		fmt.Printf("    enable: true\n")
	}
}
