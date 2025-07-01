package provider

import (
	"context"
	"time"
)

// Provider defines the interface for all LLM providers
type Provider interface {
	// Ask sends a prompt to the LLM and returns a channel of streaming responses
	Ask(ctx context.Context, prompt string, opts Options) (<-chan Response, error)

	// GetName returns the provider name for identification
	GetName() string

	// GetModel returns the model being used
	GetModel() string

	// EstimateTokens estimates token count for a given text (used for cost calculation)
	EstimateTokens(text string) int

	// Close cleans up any resources (optional)
	Close() error
}

// Options contains parameters for the LLM request
type Options struct {
	Temperature  float64 `json:"temperature"`
	MaxTokens    int     `json:"max_tokens"`
	SystemPrompt string  `json:"system_prompt,omitempty"`
	Stream       bool    `json:"stream"`
}

// Response represents a single chunk of the streaming response
type Response struct {
	// Delta contains the incremental text content
	Delta string `json:"delta"`

	// Done indicates if this is the final response
	Done bool `json:"done"`

	// TokensUsed contains token usage information (populated on final response)
	TokensUsed *TokenUsage `json:"tokens_used,omitempty"`

	// Error contains any error that occurred
	Error error `json:"error,omitempty"`

	// Metadata contains provider-specific information
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// TokenUsage tracks token consumption for cost calculation
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ProviderError represents errors specific to provider operations
type ProviderError struct {
	Provider string
	Type     ErrorType
	Message  string
	Cause    error
}

func (e *ProviderError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *ProviderError) Unwrap() error {
	return e.Cause
}

// ErrorType categorizes different types of provider errors
type ErrorType string

const (
	ErrorTypeAuth        ErrorType = "auth"         // Authentication/API key issues
	ErrorTypeRateLimit   ErrorType = "rate_limit"   // Rate limiting
	ErrorTypeQuota       ErrorType = "quota"        // Quota exceeded
	ErrorTypeTimeout     ErrorType = "timeout"      // Request timeout
	ErrorTypeNetwork     ErrorType = "network"      // Network connectivity
	ErrorTypeValidation  ErrorType = "validation"   // Invalid request parameters
	ErrorTypeServerError ErrorType = "server_error" // Provider server error
	ErrorTypeUnknown     ErrorType = "unknown"      // Unexpected error
)

// Stats contains performance and cost information for a provider request
type Stats struct {
	Provider      string        `json:"provider"`
	Model         string        `json:"model"`
	StartTime     time.Time     `json:"start_time"`
	EndTime       time.Time     `json:"end_time"`
	Duration      time.Duration `json:"duration"`
	TokensUsed    *TokenUsage   `json:"tokens_used"`
	EstimatedCost float64       `json:"estimated_cost"`
	Success       bool          `json:"success"`
	Error         error         `json:"error,omitempty"`
}

// ProviderConfig contains configuration for initializing providers
type ProviderConfig struct {
	Kind    string            `json:"kind"`
	Model   string            `json:"model"`
	BaseURL string            `json:"base_url,omitempty"`
	Host    string            `json:"host,omitempty"`
	APIKey  string            `json:"api_key,omitempty"`
	Options map[string]string `json:"options,omitempty"`
	Timeout time.Duration     `json:"timeout"`
	Retries int               `json:"retries"`
}

// Factory creates providers based on configuration
type Factory interface {
	CreateProvider(config ProviderConfig) (Provider, error)
	SupportedKinds() []string
}

// StreamCollector is a utility for collecting streaming responses
type StreamCollector struct {
	Content    string
	TokensUsed *TokenUsage
	Stats      *Stats
	Error      error
}

// NewStreamCollector creates a new stream collector
func NewStreamCollector() *StreamCollector {
	return &StreamCollector{
		Stats: &Stats{
			StartTime: time.Now(),
		},
	}
}

// Collect reads all responses from a channel and accumulates the content
func (sc *StreamCollector) Collect(ctx context.Context, responseChan <-chan Response) {
	defer func() {
		sc.Stats.EndTime = time.Now()
		sc.Stats.Duration = sc.Stats.EndTime.Sub(sc.Stats.StartTime)
	}()

	for {
		select {
		case response, ok := <-responseChan:
			if !ok {
				// Channel closed
				sc.Stats.Success = sc.Error == nil
				return
			}

			if response.Error != nil {
				sc.Error = response.Error
				sc.Stats.Error = response.Error
				sc.Stats.Success = false
				return
			}

			// Accumulate content
			sc.Content += response.Delta

			// Capture final token usage
			if response.TokensUsed != nil {
				sc.TokensUsed = response.TokensUsed
				sc.Stats.TokensUsed = response.TokensUsed
			}

			// Check if done
			if response.Done {
				sc.Stats.Success = true
				return
			}

		case <-ctx.Done():
			sc.Error = ctx.Err()
			sc.Stats.Error = ctx.Err()
			sc.Stats.Success = false
			return
		}
	}
}

// EstimateTokensSimple provides a rough token estimate (4 chars ≈ 1 token)
func EstimateTokensSimple(text string) int {
	return len(text) / 4
}

// EstimateCost calculates estimated cost based on token usage and model pricing
func EstimateCost(model string, tokens *TokenUsage) float64 {
	if tokens == nil {
		return 0
	}

	// Pricing per 1M tokens (approximate, as of 2024)
	pricing := map[string]struct {
		input  float64
		output float64
	}{
		"gpt-4o":          {5.00, 15.00},
		"gpt-4o-mini":     {0.15, 0.60},
		"gpt-4":           {30.00, 60.00},
		"gpt-3.5-turbo":   {0.50, 1.50},
		"claude-3-opus":   {15.00, 75.00},
		"claude-3-sonnet": {3.00, 15.00},
		"claude-3-haiku":  {0.25, 1.25},
	}

	prices, exists := pricing[model]
	if !exists {
		// Default to mid-range pricing if model not found
		prices = struct {
			input  float64
			output float64
		}{3.00, 15.00}
	}

	inputCost := float64(tokens.PromptTokens) * prices.input / 1_000_000
	outputCost := float64(tokens.CompletionTokens) * prices.output / 1_000_000

	return inputCost + outputCost
}

// RetryConfig defines retry behavior for provider requests
type RetryConfig struct {
	MaxAttempts  int           `json:"max_attempts"`
	InitialDelay time.Duration `json:"initial_delay"`
	MaxDelay     time.Duration `json:"max_delay"`
	Multiplier   float64       `json:"multiplier"`
}

// DefaultRetryConfig returns sensible retry defaults
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
	}
}
