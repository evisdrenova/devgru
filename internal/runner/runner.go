package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// JudgeResult represents the result from a judge evaluation
type JudgeResult struct {
	JudgeID  string        `json:"judge_id"`
	WorkerID string        `json:"worker_id"`
	Score    int           `json:"score"`
	Reason   string        `json:"reason"`
	Error    error         `json:"error"`
	Duration time.Duration `json:"duration"`
}

// WorkerResult represents the result from a single worker
type WorkerResult struct {
	WorkerID     string                 `json:"worker_id"`
	Content      string                 `json:"content"`
	TokensUsed   *provider.TokenUsage   `json:"tokens_used"`
	Stats        *provider.Stats        `json:"stats"`
	Error        error                  `json:"error"`
	Metadata     map[string]interface{} `json:"metadata"`
	JudgeResults []JudgeResult          `json:"judge_results,omitempty"`
	AverageScore float64                `json:"average_score,omitempty"`
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

// runConsensus executes the configured consensus algorithm
func (r *Runner) runConsensus(ctx context.Context, workers []WorkerResult, originalPrompt string) (*Consensus, error) {
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
		return r.scoreTop1Consensus(ctx, successfulWorkers, consensus, originalPrompt)
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

// scoreTop1Consensus implements judge-based scoring
func (r *Runner) scoreTop1Consensus(ctx context.Context, workers []WorkerResult, consensus *Consensus, originalPrompt string) (*Consensus, error) {
	if len(r.config.Judges) == 0 {
		// No judges configured, fall back to majority
		return r.majorityConsensus(workers, consensus)
	}

	// Evaluate each worker response with all judges
	evaluatedWorkers := make([]WorkerResult, len(workers))
	copy(evaluatedWorkers, workers)

	for i := range evaluatedWorkers {
		if evaluatedWorkers[i].Error == nil {
			judgeResults, err := r.evaluateWithJudges(ctx, evaluatedWorkers[i], originalPrompt)
			if err != nil {
				// Log error but don't fail consensus - we can still compare what we have
				fmt.Printf("Warning: Failed to evaluate worker %s with judges: %v\n", evaluatedWorkers[i].WorkerID, err)
			} else {
				evaluatedWorkers[i].JudgeResults = judgeResults
				evaluatedWorkers[i].AverageScore = r.calculateAverageScore(judgeResults)
			}
		}
	}

	// Find the worker with the highest average score
	var bestWorker *WorkerResult
	var bestScore float64 = -1

	for i := range evaluatedWorkers {
		worker := &evaluatedWorkers[i]
		if worker.Error == nil {
			// If we have judge scores, use them; otherwise use a default score
			score := worker.AverageScore
			if len(worker.JudgeResults) == 0 {
				score = 5.0 // Default neutral score for workers not evaluated
			}

			if score > bestScore {
				bestScore = score
				bestWorker = worker
			}
		}
	}

	if bestWorker == nil {
		return nil, fmt.Errorf("no valid workers found for scoring")
	}

	// Check if the best score meets the minimum threshold
	if bestScore < r.config.Consensus.MinScore {
		return nil, fmt.Errorf("best score %.2f does not meet minimum threshold %.2f", bestScore, r.config.Consensus.MinScore)
	}

	consensus.Winner = bestWorker.WorkerID
	consensus.Content = bestWorker.Content
	consensus.Confidence = bestScore / 10.0 // Convert 0-10 score to 0-1 confidence

	// Build reasoning
	reasoning := fmt.Sprintf("Selected %s with average score %.2f from %d judges",
		bestWorker.WorkerID, bestScore, len(r.config.Judges))

	if len(bestWorker.JudgeResults) > 0 {
		reasoning += " ("
		for i, result := range bestWorker.JudgeResults {
			if i > 0 {
				reasoning += ", "
			}
			reasoning += fmt.Sprintf("%s: %d", result.JudgeID, result.Score)
		}
		reasoning += ")"
	}

	consensus.Reasoning = reasoning

	// Update the workers slice with evaluation results
	copy(workers, evaluatedWorkers)

	return consensus, nil
}

// evaluateWithJudges evaluates a worker response with all configured judges
func (r *Runner) evaluateWithJudges(ctx context.Context, worker WorkerResult, originalPrompt string) ([]JudgeResult, error) {
	g, ctx := errgroup.WithContext(ctx)
	results := make([]JudgeResult, len(r.config.Judges))
	var mu sync.Mutex

	for i, judge := range r.config.Judges {
		i, judge := i, judge // Capture loop variables

		g.Go(func() error {
			result := r.evaluateWithSingleJudge(ctx, worker, originalPrompt, judge)

			mu.Lock()
			results[i] = result
			mu.Unlock()

			return nil // Don't fail the group if one judge fails
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Filter out failed evaluations
	var validResults []JudgeResult
	for _, result := range results {
		if result.Error == nil {
			validResults = append(validResults, result)
		}
	}

	return validResults, nil
}

// evaluateWithSingleJudge evaluates a worker response with a single judge
func (r *Runner) evaluateWithSingleJudge(ctx context.Context, worker WorkerResult, originalPrompt string, judge config.Judge) JudgeResult {
	startTime := time.Now()
	result := JudgeResult{
		JudgeID:  judge.ID,
		WorkerID: worker.WorkerID,
	}

	// Get the provider for this judge
	prov, err := r.providerManager.GetProvider(judge.Provider)
	if err != nil {
		result.Error = fmt.Errorf("failed to get judge provider %s: %w", judge.Provider, err)
		result.Duration = time.Since(startTime)
		return result
	}

	// Construct the evaluation prompt
	evaluationPrompt := fmt.Sprintf(`Original Question: %s

Response to Evaluate: %s

Please evaluate this response according to the criteria in your system prompt.`, originalPrompt, worker.Content)

	// Set up options for the judge
	opts := provider.Options{
		Temperature:  0.1, // Low temperature for consistent evaluation
		MaxTokens:    500, // Judges should be concise
		SystemPrompt: judge.SystemPrompt,
		Stream:       false, // Non-streaming for easier parsing
	}

	// Execute the evaluation
	responseChan, err := prov.Ask(ctx, evaluationPrompt, opts)
	if err != nil {
		result.Error = fmt.Errorf("failed to ask judge: %w", err)
		result.Duration = time.Since(startTime)
		return result
	}

	// Collect the response
	collector := provider.NewStreamCollector()
	collector.Collect(ctx, responseChan)

	result.Duration = time.Since(startTime)

	if collector.Error != nil {
		result.Error = collector.Error
		return result
	}

	// Parse the JSON response
	score, reason, err := r.parseJudgeResponse(collector.Content)
	if err != nil {
		result.Error = fmt.Errorf("failed to parse judge response: %w", err)
		return result
	}

	result.Score = score
	result.Reason = reason

	return result
}

// parseJudgeResponse parses the JSON response from a judge
func (r *Runner) parseJudgeResponse(response string) (int, string, error) {
	// Try to extract JSON from the response
	response = strings.TrimSpace(response)

	// Look for JSON object in the response
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")

	if start == -1 || end == -1 || end <= start {
		return 0, "", fmt.Errorf("no JSON object found in response: %s", response)
	}

	jsonStr := response[start : end+1]

	var judgeResponse struct {
		Score  int    `json:"score"`
		Reason string `json:"reason"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &judgeResponse); err != nil {
		return 0, "", fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	// Validate score range
	if judgeResponse.Score < 0 || judgeResponse.Score > 10 {
		return 0, "", fmt.Errorf("score %d is out of range (0-10)", judgeResponse.Score)
	}

	return judgeResponse.Score, judgeResponse.Reason, nil
}

// calculateAverageScore calculates the average score from judge results
func (r *Runner) calculateAverageScore(judgeResults []JudgeResult) float64 {
	if len(judgeResults) == 0 {
		return 0
	}

	var total int
	for _, result := range judgeResults {
		total += result.Score
	}

	return float64(total) / float64(len(judgeResults))
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
