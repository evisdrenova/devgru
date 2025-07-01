package runner

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/evisdrenova/devgru/internal/config"
	"github.com/evisdrenova/devgru/internal/provider"
	"github.com/evisdrenova/devgru/internal/provider/factories"
	"golang.org/x/sync/errgroup"
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

// WorkerResult represents the result from a single worker
type WorkerResult struct {
	WorkerID   string                 `json:"worker_id"`
	Content    string                 `json:"content"`
	TokensUsed *provider.TokenUsage   `json:"tokens_used"`
	Stats      *provider.Stats        `json:"stats"`
	Error      error                  `json:"error"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// RunResult contains the results from all workers
type RunResult struct {
	Prompt        string         `json:"prompt"`
	Workers       []WorkerResult `json:"workers"`
	Consensus     *Consensus     `json:"consensus"`
	TotalDuration time.Duration  `json:"total_duration"`
	TotalTokens   int            `json:"total_tokens"`
	EstimatedCost float64        `json:"estimated_cost"`
	Success       bool           `json:"success"`
	StartTime     time.Time      `json:"start_time"`
	EndTime       time.Time      `json:"end_time"`
}

// Consensus represents the final consensus result
type Consensus struct {
	Algorithm    string  `json:"algorithm"`
	Winner       string  `json:"winner"`       // Worker ID of winning response
	Content      string  `json:"content"`      // Final consensus content
	Confidence   float64 `json:"confidence"`   // Confidence score (0-1)
	Reasoning    string  `json:"reasoning"`    // Why this consensus was chosen
	Participants int     `json:"participants"` // Number of workers that succeeded
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
	consensus, err := r.runConsensus(workerResults)
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

	// Update stats with provider info
	if result.Stats != nil {
		result.Stats.Provider = prov.GetName()
		result.Stats.Model = prov.GetModel()
	} else {
		result.Stats = stats
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

// runConsensus executes the configured consensus algorithm
func (r *Runner) runConsensus(workers []WorkerResult) (*Consensus, error) {
	// Filter out failed workers
	successfulWorkers := make([]WorkerResult, 0, len(workers))
	for _, worker := range workers {
		if worker.Error == nil && worker.Content != "" {
			successfulWorkers = append(successfulWorkers, worker)
		}
	}

	if len(successfulWorkers) == 0 {
		return nil, fmt.Errorf("no successful workers to build consensus from")
	}

	consensus := &Consensus{
		Algorithm:    r.config.Consensus.Algorithm,
		Participants: len(successfulWorkers),
	}

	switch r.config.Consensus.Algorithm {
	case "majority":
		return r.majorityConsensus(successfulWorkers, consensus)
	case "score_top1":
		return r.scoreTop1Consensus(successfulWorkers, consensus)
	case "embedding_cluster":
		return nil, fmt.Errorf("embedding_cluster consensus not yet implemented")
	case "referee":
		return nil, fmt.Errorf("referee consensus not yet implemented")
	default:
		return nil, fmt.Errorf("unknown consensus algorithm: %s", r.config.Consensus.Algorithm)
	}
}

// majorityConsensus implements simple majority voting (for now, just picks the first)
func (r *Runner) majorityConsensus(workers []WorkerResult, consensus *Consensus) (*Consensus, error) {
	if len(workers) == 0 {
		return nil, fmt.Errorf("no workers for majority consensus")
	}

	// For now, implement a simple "first successful response" approach
	// TODO: Implement actual similarity-based majority voting
	winner := workers[0]

	consensus.Winner = winner.WorkerID
	consensus.Content = winner.Content
	consensus.Confidence = 1.0 / float64(len(workers)) // Simple confidence based on participation
	consensus.Reasoning = fmt.Sprintf("Selected response from %s (simple majority algorithm)", winner.WorkerID)

	return consensus, nil
}

// scoreTop1Consensus implements judge-based scoring (placeholder for now)
func (r *Runner) scoreTop1Consensus(workers []WorkerResult, consensus *Consensus) (*Consensus, error) {
	// TODO: Implement actual judge-based scoring
	// For now, fall back to majority consensus
	return r.majorityConsensus(workers, consensus)
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
