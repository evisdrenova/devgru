package runner

import (
	"time"

	"github.com/evisdrenova/devgru/internal/provider"
)

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

// PlanStepType represents the type of a plan step
type PlanStepType string

const (
	PlanStepRead   PlanStepType = "read"
	PlanStepUpdate PlanStepType = "update"
	PlanStepCreate PlanStepType = "create"
	PlanStepDelete PlanStepType = "delete"
)

// PlanStep represents a single step in a plan
type PlanStep struct {
	Number      int          `json:"number"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Type        PlanStepType `json:"type"`
	Files       []string     `json:"files"`
}

// PlanResult represents the result of a planning phase
type PlanResult struct {
	TargetFile   string     `json:"target_file"`
	Steps        []PlanStep `json:"steps"`
	SelectedPlan string     `json:"selected_plan"`
	Confidence   float64    `json:"confidence"`
	Reasoning    string     `json:"reasoning"`
}
