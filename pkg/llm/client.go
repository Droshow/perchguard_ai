package llm

import (
	"context"
	"time"
)

// Client is the narrow interface the SemanticFirewall uses to call an LLM.
// Accepting an interface (not a concrete type) keeps the validator testable
// without a real API key.
type Client interface {
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

type CompletionRequest struct {
	SystemPrompt string
	UserMessage  string
	MaxTokens    int
	Temperature  float64
	// Model overrides the client's default model for this request.
	// Empty string = use client default.
	Model string
}

type CompletionResponse struct {
	Content      string
	InputTokens  int
	OutputTokens int
}

// Option is a functional option for claudeClient construction.
type Option func(*claudeClient)

func WithBaseURL(url string) Option {
	return func(c *claudeClient) { c.baseURL = url }
}

// WithTimeout sets the default context budget applied when the caller
// provides a context with no deadline. Does NOT cap the HTTP transport
// timeout (that is always 10s to handle network variance).
func WithTimeout(d time.Duration) Option {
	return func(c *claudeClient) { c.defaultBudget = d }
}

// WithModel overrides the default model.
func WithModel(m string) Option {
	return func(c *claudeClient) { c.model = m }
}
