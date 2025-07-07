package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/evisdrenova/devgru/internal/config"
	"github.com/evisdrenova/devgru/internal/ide"
	"github.com/evisdrenova/devgru/internal/provider"
	"github.com/evisdrenova/devgru/internal/provider/factories"
)

// Runner orchestrates multiple workers to process prompts
type Runner struct {
	config          *config.Config
	providerManager *factories.ProviderManager
}

// NewRunner creates a new runner instance
func NewRunner(cfg *config.Config) (*Runner, error) {
	factory := factories.NewDefaultFactory()
	providerManager := factories.NewProviderManager(factory)

	// Convert config providers to provider configs
	providerConfigs := make(map[string]provider.ProviderConfig)
	for name, configProvider := range cfg.Providers {
		providerConfigs[name] = provider.ProviderConfig{
			Kind:    configProvider.Kind,
			Model:   configProvider.Model,
			BaseURL: configProvider.BaseURL,
			Host:    configProvider.Host,
			APIKey:  configProvider.APIKey,
			Timeout: cfg.Consensus.Timeout,
		}
	}

	// Create all providers
	if err := providerManager.CreateProviders(providerConfigs); err != nil {
		return nil, fmt.Errorf("failed to create providers: %w", err)
	}

	return &Runner{
		config:          cfg,
		providerManager: providerManager,
	}, nil
}

// Run executes the prompt across all configured workers
func (r *Runner) Run(ctx context.Context, prompt string) (*RunResult, error) {
	startTime := time.Now()

	result := &RunResult{
		Prompt:    prompt,
		Workers:   make([]WorkerResult, 0, len(r.config.Workers)),
		StartTime: startTime,
	}

	// Create a context with timeout
	runCtx, cancel := context.WithTimeout(ctx, r.config.Consensus.Timeout)
	defer cancel()

	// Fan out to all workers concurrently
	workerResults, err := r.runWorkers(runCtx, prompt)
	if err != nil {
		result.Success = false
		result.EndTime = time.Now()
		result.TotalDuration = result.EndTime.Sub(result.StartTime)
		return result, fmt.Errorf("failed to run workers: %w", err)
	}

	result.Workers = workerResults

	// Calculate aggregate stats
	r.calculateAggregateStats(result)

	// Run consensus algorithm
	consensus, err := r.runConsensus(runCtx, workerResults, prompt)
	if err != nil {
		// Even if consensus fails, we still return the worker results
		result.Success = false
		result.EndTime = time.Now()
		result.TotalDuration = result.EndTime.Sub(result.StartTime)
		return result, fmt.Errorf("consensus failed: %w", err)
	}

	result.Consensus = consensus
	result.Success = true
	result.EndTime = time.Now()
	result.TotalDuration = result.EndTime.Sub(result.StartTime)

	return result, nil
}

// runWorkers executes the prompt across all workers concurrently
func (r *Runner) runWorkers(ctx context.Context, prompt string) ([]WorkerResult, error) {
	g, ctx := errgroup.WithContext(ctx)
	results := make([]WorkerResult, len(r.config.Workers))
	var mu sync.Mutex

	for i, worker := range r.config.Workers {
		i, worker := i, worker // Capture loop variables

		g.Go(func() error {
			result := r.runSingleWorker(ctx, worker, prompt)

			mu.Lock()
			results[i] = result
			mu.Unlock()

			return nil // Don't fail the group if one worker fails
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return results, nil
}

// runSingleWorker executes the prompt on a single worker
func (r *Runner) runSingleWorker(ctx context.Context, worker config.Worker, prompt string) WorkerResult {
	result := WorkerResult{
		WorkerID: worker.ID,
		Metadata: make(map[string]interface{}),
	}

	// Get the provider for this worker
	prov, err := r.providerManager.GetProvider(worker.Provider)
	if err != nil {
		result.Error = fmt.Errorf("failed to get provider %s: %w", worker.Provider, err)
		return result
	}

	// Set up options for the provider
	opts := provider.Options{
		Temperature:  worker.Temperature,
		MaxTokens:    worker.MaxTokens,
		SystemPrompt: worker.SystemPrompt,
		Stream:       true, // Always use streaming for better UX
	}

	// Create stats tracking
	stats := &provider.Stats{
		Provider:  prov.GetName(),
		Model:     prov.GetModel(),
		StartTime: time.Now(),
	}

	// Execute the request
	responseChan, err := prov.Ask(ctx, prompt, opts)
	if err != nil {
		result.Error = fmt.Errorf("failed to ask provider: %w", err)
		result.Stats = stats
		return result
	}

	// Collect the streaming response
	collector := provider.NewStreamCollector()
	collector.Collect(ctx, responseChan)

	// Populate result
	result.Content = collector.Content
	result.TokensUsed = collector.TokensUsed
	result.Error = collector.Error
	result.Stats = collector.Stats

	// If we don't have token usage from the API, estimate it
	if result.TokensUsed == nil && result.Error == nil && result.Content != "" {
		promptTokens := prov.EstimateTokens(prompt + opts.SystemPrompt)
		completionTokens := prov.EstimateTokens(result.Content)

		result.TokensUsed = &provider.TokenUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		}
	}

	// Update stats with provider info
	if result.Stats != nil {
		result.Stats.Provider = prov.GetName()
		result.Stats.Model = prov.GetModel()
		if result.TokensUsed != nil {
			result.Stats.TokensUsed = result.TokensUsed
		}
	} else {
		result.Stats = stats
		result.Stats.Provider = prov.GetName()
		result.Stats.Model = prov.GetModel()
		if result.TokensUsed != nil {
			result.Stats.TokensUsed = result.TokensUsed
		}
	}

	// Calculate estimated cost
	if result.TokensUsed != nil {
		result.Stats.EstimatedCost = provider.EstimateCost(prov.GetModel(), result.TokensUsed)
	}

	// Add metadata
	result.Metadata["provider_kind"] = r.config.Providers[worker.Provider].Kind
	result.Metadata["temperature"] = worker.Temperature
	result.Metadata["max_tokens"] = worker.MaxTokens

	return result
}

// calculateAggregateStats calculates totals across all workers
func (r *Runner) calculateAggregateStats(result *RunResult) {
	var totalTokens int
	var totalCost float64

	for _, worker := range result.Workers {
		if worker.TokensUsed != nil {
			totalTokens += worker.TokensUsed.TotalTokens
		}
		if worker.Stats != nil {
			totalCost += worker.Stats.EstimatedCost
		}
	}

	result.TotalTokens = totalTokens
	result.EstimatedCost = totalCost
}

// savePlanToFile saves the generated plan to a markdown file
func (r *Runner) savePlanToFile(prompt, planContent string) error {
	// Create a filename based on timestamp
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("plan_%s.md", timestamp)

	// Create plans directory if it doesn't exist
	plansDir := "plans"
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		return fmt.Errorf("failed to create plans directory: %w", err)
	}

	filepath := filepath.Join(plansDir, filename)

	// Create the markdown content
	markdownContent := fmt.Sprintf(`# Implementation Plan

**Generated:** %s

**Request:** %s

---

%s
`,
		time.Now().Format("2006-01-02 15:04:05"),
		prompt,
		planContent)

	// Write to file
	if err := os.WriteFile(filepath, []byte(markdownContent), 0644); err != nil {
		return fmt.Errorf("failed to write plan file: %w", err)
	}

	fmt.Printf("📋 Plan saved to: %s\n", filepath)
	return nil
}

// Close cleans up the runner and its resources
func (r *Runner) Close() error {
	return r.providerManager.CloseAll()
}

// GetStats returns current runner statistics
func (r *Runner) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"providers": len(r.config.Providers),
		"workers":   len(r.config.Workers),
		"judges":    len(r.config.Judges),
		"algorithm": r.config.Consensus.Algorithm,
	}
}

// GeneratePlan uses the configured workers to generate a plan for the given prompt
func (r *Runner) GeneratePlan(prompt string, ideContext interface{}) (*PlanResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), r.config.Consensus.Timeout)
	defer cancel()

	// Use the first worker to generate the plan
	if len(r.config.Workers) == 0 {
		return nil, fmt.Errorf("no workers configured")
	}

	worker := r.config.Workers[0]

	// Get the provider for this worker
	prov, err := r.providerManager.GetProvider(worker.Provider)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider %s: %w", worker.Provider, err)
	}

	// Build comprehensive context
	contextInfo := r.buildProjectContext(ideContext)

	// Create a planning-specific prompt with project context
	planningPrompt := fmt.Sprintf(`Please analyze the following request and create a comprehensive implementation plan:

## Request
%s

## Project Context
%s

## Instructions
Create a detailed implementation plan with:
1. **Analysis**: What needs to be done and why (considering current project state)
2. **Implementation Steps**: Detailed step-by-step approach
3. **Files/Components**: What files or components will be affected
4. **Testing Strategy**: How to verify the implementation
5. **Action Items**: A numbered list of specific todos that need to be completed

## Important Requirements
- Consider the current project structure and files
- Take into account any existing code, errors, or diagnostics
- If modifying existing files, explain what changes are needed and why
- End your response with a clear "## Action Items" section containing specific, actionable todos
- Each action item should be a single, concrete task that can be completed

Format your response as a clear, structured markdown plan.`, prompt, contextInfo)

	// Set up options for the provider
	opts := provider.Options{
		Temperature:  0.3, // Lower temperature for more consistent planning
		MaxTokens:    worker.MaxTokens,
		SystemPrompt: "You are a helpful coding assistant that creates detailed implementation plans. Always provide structured, actionable plans in markdown format.",
		Stream:       false, // Don't stream for planning
	}

	// Execute the request
	responseChan, err := prov.Ask(ctx, planningPrompt, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to ask provider: %w", err)
	}

	// Collect the response
	collector := provider.NewStreamCollector()
	collector.Collect(ctx, responseChan)

	if collector.Error != nil {
		return nil, collector.Error
	}

	// Extract todos from the generated plan
	todos := r.extractTodosFromPlan(collector.Content)

	// Save the plan to a markdown file
	if err := r.savePlanToFile(prompt, collector.Content); err != nil {
		// Log the error but don't fail the planning process
		fmt.Printf("Warning: Could not save plan to file: %v\n", err)
	}

	// Create enhanced steps from todos
	planSteps := r.convertTodosToSteps(todos)

	// Create a structured plan result
	plan := &PlanResult{
		TargetFile:   r.extractTargetFileFromContext(ideContext),
		Steps:        planSteps,
		SelectedPlan: prov.GetModel(),
		Confidence:   0.85,
		Reasoning:    collector.Content,
		Todos:        todos, // Add todos to the plan result
	}

	return plan, nil
}

// buildProjectContext creates a comprehensive context string from IDE information
func (r *Runner) buildProjectContext(ideContext interface{}) string {
	if ideContext == nil {
		return "No project context available."
	}

	var contextParts []string

	// Type assertion to access IDE context fields
	if ctx, ok := ideContext.(*ide.IDEContext); ok {
		// Active file information
		if ctx.ActiveFile != "" {
			contextParts = append(contextParts, fmt.Sprintf("**Active File**: %s", ctx.ActiveFile))
		}

		// Selected text information
		if ctx.Selection != nil && ctx.Selection.Text != "" {
			contextParts = append(contextParts, fmt.Sprintf("**Selected Code** (lines %d-%d):\n```%s\n%s\n```", 
				ctx.Selection.StartLine, ctx.Selection.EndLine, ctx.Selection.Language, ctx.Selection.Text))
		}

		// Workspace information
		if ctx.WorkspaceRoot != "" {
			contextParts = append(contextParts, fmt.Sprintf("**Workspace**: %s", ctx.WorkspaceRoot))
		}

		// Open files
		if len(ctx.OpenFiles) > 0 {
			openFilesStr := strings.Join(ctx.OpenFiles, ", ")
			if len(openFilesStr) > 200 { // Truncate if too long
				openFilesStr = openFilesStr[:200] + "..."
			}
			contextParts = append(contextParts, fmt.Sprintf("**Open Files**: %s", openFilesStr))
		}

		// Diagnostics (errors/warnings)
		if len(ctx.Diagnostics) > 0 {
			var diagStrings []string
			for i, diag := range ctx.Diagnostics {
				if i >= 5 { // Limit to first 5 diagnostics
					diagStrings = append(diagStrings, "...")
					break
				}
				diagStrings = append(diagStrings, fmt.Sprintf("- %s:%d: [%s] %s", 
					diag.File, diag.Line, diag.Severity, diag.Message))
			}
			if len(diagStrings) > 0 {
				contextParts = append(contextParts, fmt.Sprintf("**Current Issues**:\n%s", strings.Join(diagStrings, "\n")))
			}
		}
	}

	if len(contextParts) == 0 {
		return "No specific project context available."
	}

	return strings.Join(contextParts, "\n\n")
}

// extractTodosFromPlan extracts action items from the generated plan
func (r *Runner) extractTodosFromPlan(planContent string) []string {
	var todos []string
	
	// Look for "Action Items" or "TODO" sections
	lines := strings.Split(planContent, "\n")
	inTodoSection := false
	
	// Regex patterns to match todo items
	todoSectionPattern := regexp.MustCompile(`(?i)^##?\s*(action\s+items?|todos?|tasks?)`)
	listItemPattern := regexp.MustCompile(`^\s*(\d+\.|[-*+])\s+(.+)$`)
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		
		// Check if we're entering a todo section
		if todoSectionPattern.MatchString(line) {
			inTodoSection = true
			continue
		}
		
		// Check if we're leaving the todo section (new heading)
		if inTodoSection && strings.HasPrefix(line, "#") && !todoSectionPattern.MatchString(line) {
			inTodoSection = false
			continue
		}
		
		// Extract todo items
		if inTodoSection && listItemPattern.MatchString(line) {
			matches := listItemPattern.FindStringSubmatch(line)
			if len(matches) > 2 {
				todo := strings.TrimSpace(matches[2])
				if todo != "" {
					todos = append(todos, todo)
				}
			}
		}
	}
	
	// If no explicit todo section found, look for numbered lists throughout the document
	if len(todos) == 0 {
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if matches := regexp.MustCompile(`^\d+\.\s+(.+)$`).FindStringSubmatch(line); len(matches) > 1 {
				todo := strings.TrimSpace(matches[1])
				if todo != "" && !strings.Contains(strings.ToLower(todo), "analysis") {
					todos = append(todos, todo)
				}
			}
		}
	}
	
	return todos
}

// convertTodosToSteps converts extracted todos into PlanStep format
func (r *Runner) convertTodosToSteps(todos []string) []PlanStep {
	steps := make([]PlanStep, len(todos))
	
	for i, todo := range todos {
		stepType := PlanStepUpdate // Default type
		
		// Determine step type based on todo content
		todoLower := strings.ToLower(todo)
		if strings.Contains(todoLower, "read") || strings.Contains(todoLower, "analyze") || strings.Contains(todoLower, "review") {
			stepType = PlanStepRead
		} else if strings.Contains(todoLower, "create") || strings.Contains(todoLower, "add") || strings.Contains(todoLower, "new") {
			stepType = PlanStepCreate
		} else if strings.Contains(todoLower, "delete") || strings.Contains(todoLower, "remove") {
			stepType = PlanStepDelete
		}
		
		steps[i] = PlanStep{
			Number: i + 1,
			Title:  todo,
			Type:   stepType,
		}
	}
	
	// If no todos found, provide default steps
	if len(steps) == 0 {
		steps = []PlanStep{
			{Number: 1, Title: "Analyze and understand requirements", Type: PlanStepRead},
			{Number: 2, Title: "Implement the solution", Type: PlanStepUpdate},
		}
	}
	
	return steps
}

// extractTargetFileFromContext attempts to determine the target file from context
func (r *Runner) extractTargetFileFromContext(ideContext interface{}) string {
	if ideContext == nil {
		return "based on context"
	}

	if ctx, ok := ideContext.(*ide.IDEContext); ok {
		if ctx.ActiveFile != "" {
			return ctx.ActiveFile
		}
	}
	
	return "based on context"
}

// ExecutePlan executes the given plan using the configured workers
func (r *Runner) ExecutePlan(plan *PlanResult, ideContext interface{}) (*RunResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), r.config.Consensus.Timeout)
	defer cancel()

	// Create an execution prompt based on the plan
	executionPrompt := fmt.Sprintf(`Execute the following plan:

Plan: %s

Reasoning: %s

Please implement the solution step by step.`, plan.SelectedPlan, plan.Reasoning)

	// Use the existing Run method to execute the plan
	return r.Run(ctx, executionPrompt)
}
