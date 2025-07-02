package runner

import (
	"context"
	"fmt"
)

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
