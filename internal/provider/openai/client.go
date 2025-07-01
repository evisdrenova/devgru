package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/evisdrenova/devgru/internal/provider"
)

// Client implements the Provider interface for OpenAI
type Client struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
	name       string
}

// NewClient creates a new OpenAI provider client
func NewClient(config provider.ProviderConfig) (*Client, error) {
	if config.APIKey == "" {
		return nil, &provider.ProviderError{
			Provider: "openai",
			Type:     provider.ErrorTypeAuth,
			Message:  "API key is required",
		}
	}

	if config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com/v1"
	}

	timeout := config.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	return &Client{
		baseURL: config.BaseURL,
		apiKey:  config.APIKey,
		model:   config.Model,
		name:    fmt.Sprintf("openai-%s", config.Model),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

// Ask implements the Provider interface
func (c *Client) Ask(ctx context.Context, prompt string, opts provider.Options) (<-chan provider.Response, error) {
	responseChan := make(chan provider.Response, 10)

	go func() {
		defer close(responseChan)
		c.streamRequest(ctx, prompt, opts, responseChan)
	}()

	return responseChan, nil
}

// GetName returns the provider name
func (c *Client) GetName() string {
	return c.name
}

// GetModel returns the model name
func (c *Client) GetModel() string {
	return c.model
}

// EstimateTokens provides a rough token estimate
func (c *Client) EstimateTokens(text string) int {
	// More sophisticated estimation could use tiktoken-go
	return provider.EstimateTokensSimple(text)
}

// Close cleans up resources
func (c *Client) Close() error {
	return nil
}

// streamRequest handles the actual streaming request to OpenAI
func (c *Client) streamRequest(ctx context.Context, prompt string, opts provider.Options, responseChan chan<- provider.Response) {
	reqBody := c.buildRequestBody(prompt, opts)

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		responseChan <- provider.Response{
			Error: &provider.ProviderError{
				Provider: "openai",
				Type:     provider.ErrorTypeValidation,
				Message:  "failed to marshal request",
				Cause:    err,
			},
		}
		return
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(reqBytes))
	if err != nil {
		responseChan <- provider.Response{
			Error: &provider.ProviderError{
				Provider: "openai",
				Type:     provider.ErrorTypeValidation,
				Message:  "failed to create request",
				Cause:    err,
			},
		}
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if opts.Stream {
		req.Header.Set("Accept", "text/event-stream")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		responseChan <- provider.Response{
			Error: &provider.ProviderError{
				Provider: "openai",
				Type:     provider.ErrorTypeNetwork,
				Message:  "request failed",
				Cause:    err,
			},
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.handleErrorResponse(resp, responseChan)
		return
	}

	if opts.Stream {
		c.handleStreamingResponse(resp.Body, responseChan)
	} else {
		c.handleNonStreamingResponse(resp.Body, responseChan)
	}
}

// buildRequestBody constructs the OpenAI API request body
func (c *Client) buildRequestBody(prompt string, opts provider.Options) map[string]interface{} {
	messages := []map[string]string{
		{
			"role":    "user",
			"content": prompt,
		},
	}

	// Add system message if provided
	if opts.SystemPrompt != "" {
		messages = append([]map[string]string{
			{
				"role":    "system",
				"content": opts.SystemPrompt,
			},
		}, messages...)
	}

	reqBody := map[string]interface{}{
		"model":       c.model,
		"messages":    messages,
		"temperature": opts.Temperature,
		"stream":      opts.Stream,
	}

	if opts.MaxTokens > 0 {
		reqBody["max_tokens"] = opts.MaxTokens
	}

	return reqBody
}

// handleStreamingResponse processes Server-Sent Events from OpenAI
func (c *Client) handleStreamingResponse(body io.Reader, responseChan chan<- provider.Response) {
	scanner := bufio.NewScanner(body)
	var totalTokens *provider.TokenUsage

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || line == "data: [DONE]" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// Skip malformed chunks
			continue
		}

		// Process the chunk
		if len(chunk.Choices) > 0 {
			choice := chunk.Choices[0]

			// Send content delta
			if choice.Delta.Content != "" {
				responseChan <- provider.Response{
					Delta: choice.Delta.Content,
					Done:  false,
				}
			}

			// Check for completion
			if choice.FinishReason != nil {
				// This is the final chunk, get usage info
				if chunk.Usage != nil {
					totalTokens = &provider.TokenUsage{
						PromptTokens:     chunk.Usage.PromptTokens,
						CompletionTokens: chunk.Usage.CompletionTokens,
						TotalTokens:      chunk.Usage.TotalTokens,
					}
				}

				responseChan <- provider.Response{
					Delta:      "",
					Done:       true,
					TokensUsed: totalTokens,
				}
				return
			}
		}
	}

	if err := scanner.Err(); err != nil {
		responseChan <- provider.Response{
			Error: &provider.ProviderError{
				Provider: "openai",
				Type:     provider.ErrorTypeNetwork,
				Message:  "error reading stream",
				Cause:    err,
			},
		}
	}
}

// handleNonStreamingResponse processes a complete response from OpenAI
func (c *Client) handleNonStreamingResponse(body io.Reader, responseChan chan<- provider.Response) {
	var response openAIResponse

	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		responseChan <- provider.Response{
			Error: &provider.ProviderError{
				Provider: "openai",
				Type:     provider.ErrorTypeNetwork,
				Message:  "failed to read response body",
				Cause:    err,
			},
		}
		return
	}

	if err := json.Unmarshal(bodyBytes, &response); err != nil {
		responseChan <- provider.Response{
			Error: &provider.ProviderError{
				Provider: "openai",
				Type:     provider.ErrorTypeValidation,
				Message:  "failed to parse response",
				Cause:    err,
			},
		}
		return
	}

	if len(response.Choices) == 0 {
		responseChan <- provider.Response{
			Error: &provider.ProviderError{
				Provider: "openai",
				Type:     provider.ErrorTypeServerError,
				Message:  "no choices in response",
			},
		}
		return
	}

	content := response.Choices[0].Message.Content
	var tokenUsage *provider.TokenUsage

	if response.Usage != nil {
		tokenUsage = &provider.TokenUsage{
			PromptTokens:     response.Usage.PromptTokens,
			CompletionTokens: response.Usage.CompletionTokens,
			TotalTokens:      response.Usage.TotalTokens,
		}
	}

	// Send the complete content as a single response
	responseChan <- provider.Response{
		Delta:      content,
		Done:       true,
		TokensUsed: tokenUsage,
	}
}

// handleErrorResponse processes error responses from OpenAI
func (c *Client) handleErrorResponse(resp *http.Response, responseChan chan<- provider.Response) {
	bodyBytes, _ := io.ReadAll(resp.Body)

	var errorResp openAIErrorResponse
	json.Unmarshal(bodyBytes, &errorResp)

	errorType := provider.ErrorTypeServerError
	message := fmt.Sprintf("HTTP %d", resp.StatusCode)

	switch resp.StatusCode {
	case 401:
		errorType = provider.ErrorTypeAuth
		message = "invalid API key"
	case 429:
		errorType = provider.ErrorTypeRateLimit
		message = "rate limit exceeded"
	case 400:
		errorType = provider.ErrorTypeValidation
		message = "invalid request"
	}

	if errorResp.Error.Message != "" {
		message = errorResp.Error.Message
	}

	responseChan <- provider.Response{
		Error: &provider.ProviderError{
			Provider: "openai",
			Type:     errorType,
			Message:  message,
		},
	}
}

// OpenAI API response structures
type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type openAIErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}
