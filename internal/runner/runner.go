package runner

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/evisdrenova/devgru/internal/config"
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
