package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/evisdrenova/devgru/internal/config"
	"github.com/evisdrenova/devgru/internal/ide"
	"github.com/evisdrenova/devgru/internal/runner"
	"github.com/evisdrenova/devgru/ui"
)

func main() {
	// If no arguments, start interactive mode with auto IDE integration
	if len(os.Args) == 1 {
		runInteractiveMode()
		return
	}

	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s [command] [args...]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nCommands:\n")
		fmt.Fprintf(os.Stderr, "  (no args)        Start interactive mode with VS Code integration\n")
		fmt.Fprintf(os.Stderr, "  run <prompt>     Run a prompt through all workers\n")
		fmt.Fprintf(os.Stderr, "  run --raw <prompt>  Run with raw JSON output\n")
		fmt.Fprintf(os.Stderr, "  ide <subcommand> IDE integration commands\n")
		fmt.Fprintf(os.Stderr, "  version          Show version information\n")
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "run":
		runCommand(os.Args[2:])
	case "ide":
		ideCommand(os.Args[2:])
	case "version":
		versionCommand()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		os.Exit(1)
	}
}

// runInteractiveMode starts the interactive TUI mode with auto IDE server
func runInteractiveMode() {
	// Load configuration
	cfg, err := config.LoadDefault()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		fmt.Fprintf(os.Stderr, "Make sure you have a devgru.yaml file in the current directory or ~/.devgru/\n")
		os.Exit(1)
	}

	// Create runner
	r, err := runner.NewRunner(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create runner: %v\n", err)
		os.Exit(1)
	}
	defer r.Close()

	// Always create and start IDE server when in interactive mode
	var ideServer *ide.Server

	// If IDE is disabled in config, enable it temporarily for interactive mode
	ideConfig := ide.Config{
		Enable:    true, // Always enable for interactive mode
		Transport: cfg.IDE.Transport,
		DiffTool:  cfg.IDE.DiffTool,
		Port:      cfg.IDE.Port,
	}

	// Use default port if not configured
	if ideConfig.Port == 0 {
		ideConfig.Port = 8123
	}

	ideServer = ide.NewServer(ideConfig)

	// Start IDE server in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := ideServer.Start(ctx); err != nil {
			// Don't exit on IDE server error, just log it
			fmt.Printf("IDE server warning: %v\n", err)
		}
	}()

	// Give server time to start and print connection info
	time.Sleep(200 * time.Millisecond)

	// Create the interactive model with runner, config, and IDE server
	model := ui.NewInteractiveModel(r, cfg, ideServer)

	// Create bubbletea program with proper options
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

func runCommand(args []string) {
	var rawOutput bool
	var prompt string

	// Parse flags
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: devgru run [--raw] <prompt>\n")
		os.Exit(1)
	}

	if args[0] == "--raw" {
		rawOutput = true
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: devgru run --raw <prompt>\n")
			os.Exit(1)
		}
		prompt = args[1]
	} else {
		prompt = args[0]
	}

	// Load configuration
	cfg, err := config.LoadDefault()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		fmt.Fprintf(os.Stderr, "Make sure you have a devgru.yaml file in the current directory or ~/.devgru/\n")
		os.Exit(1)
	}

	// Create runner
	r, err := runner.NewRunner(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create runner: %v\n", err)
		os.Exit(1)
	}
	defer r.Close()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Consensus.Timeout+10*time.Second)
	defer cancel()

	if !rawOutput {
		fmt.Printf("Running prompt: %q\n", prompt)
		fmt.Printf("Workers: %d, Algorithm: %s\n\n", len(cfg.Workers), cfg.Consensus.Algorithm)
	}

	// Execute the run
	result, err := r.Run(ctx, prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to run: %v\n", err)
		os.Exit(1)
	}

	// Display results
	if rawOutput {
		rawOutputCommand(result)
	} else {
		// Use the TUI for interactive display
		model := ui.NewResultsModel(result)
		p := tea.NewProgram(model, tea.WithAltScreen())

		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
			// Fallback to simple display
			displayResultsSimple(result)
			os.Exit(1)
		}
	}
}

func displayResultsSimple(result *runner.RunResult) {
	fmt.Printf("=== RESULTS ===\n")
	fmt.Printf("Duration: %v\n", result.TotalDuration)
	fmt.Printf("Total Tokens: %d\n", result.TotalTokens)
	fmt.Printf("Estimated Cost: $%.6f\n", result.EstimatedCost)
	fmt.Printf("Success: %t\n\n", result.Success)

	// Show individual worker results
	fmt.Printf("=== WORKER RESPONSES ===\n")
	for i, worker := range result.Workers {
		fmt.Printf("[%d] %s", i+1, worker.WorkerID)
		if worker.Stats != nil {
			fmt.Printf(" (%s, %v)", worker.Stats.Model, worker.Stats.Duration)
		}
		fmt.Printf("\n")

		if worker.Error != nil {
			fmt.Printf("❌ Error: %v\n", worker.Error)
		} else {
			fmt.Printf("✅ Success")
			if worker.TokensUsed != nil {
				fmt.Printf(" (%d tokens, $%.6f)", worker.TokensUsed.TotalTokens, worker.Stats.EstimatedCost)
			}
			fmt.Printf("\n")
			fmt.Printf("Response: %s\n", worker.Content)
		}
		fmt.Printf("\n")
	}

	// Show consensus
	if result.Consensus != nil {
		fmt.Printf("=== CONSENSUS ===\n")
		fmt.Printf("Algorithm: %s\n", result.Consensus.Algorithm)
		fmt.Printf("Winner: %s\n", result.Consensus.Winner)
		fmt.Printf("Confidence: %.2f\n", result.Consensus.Confidence)
		fmt.Printf("Reasoning: %s\n", result.Consensus.Reasoning)
		fmt.Printf("Final Answer: %s\n", result.Consensus.Content)
	}
}

func versionCommand() {
	version := "devgru v0.1.0-dev"
	fmt.Println(version)
}

// rawOutputCommand outputs JSON for scripting (future enhancement)
func rawOutputCommand(result *runner.RunResult) {
	jsonBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal result: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(jsonBytes))
}
