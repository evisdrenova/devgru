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
)

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
	score, reason, err := parseJudgeResponse(collector.Content)
	if err != nil {
		result.Error = fmt.Errorf("failed to parse judge response: %w", err)
		return result
	}

	result.Score = score
	result.Reason = reason

	return result
}

// parseJudgeResponse parses the JSON response from a judge
func parseJudgeResponse(response string) (int, string, error) {
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
