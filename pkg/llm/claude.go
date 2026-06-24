package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	defaultBaseURL     = "https://api.anthropic.com"
	defaultModel       = "claude-haiku-4-5"
	defaultBudget      = 180 * time.Millisecond
	anthropicVersion   = "2023-06-01"
)

type claudeClient struct {
	apiKey        string
	model         string
	baseURL       string
	defaultBudget time.Duration
	http          *http.Client
}

// NewClaudeClient returns a Client backed by the Anthropic Messages API.
// Uses plain net/http — no SDK dependency.
func NewClaudeClient(apiKey string, opts ...Option) Client {
	c := &claudeClient{
		apiKey:        apiKey,
		model:         defaultModel,
		baseURL:       defaultBaseURL,
		defaultBudget: defaultBudget,
	}
	for _, o := range opts {
		o(c)
	}
	// HTTP transport has a generous timeout; context deadline is the real budget control.
	c.http = &http.Client{Timeout: 10 * time.Second}
	return c
}

// Complete sends a prompt and returns the model's response.
// If ctx has no deadline, applies c.defaultBudget as a floor.
// Returns an error on any failure — callers must treat errors as fail-open.
func (c *claudeClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.defaultBudget)
		defer cancel()
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 150
	}

	model := c.model
	if req.Model != "" {
		model = req.Model
	}

	body, err := json.Marshal(apiRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    req.SystemPrompt,
		Messages:  []apiMessage{{Role: "user", Content: req.UserMessage}},
	})
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	var ar apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return CompletionResponse{}, fmt.Errorf("decode: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		if ar.Error != nil {
			return CompletionResponse{}, fmt.Errorf("api error: %s", ar.Error.Message)
		}
		return CompletionResponse{}, fmt.Errorf("http %d", resp.StatusCode)
	}
	if len(ar.Content) == 0 {
		return CompletionResponse{}, fmt.Errorf("empty content")
	}
	return CompletionResponse{
		Content:      ar.Content[0].Text,
		InputTokens:  ar.Usage.InputTokens,
		OutputTokens: ar.Usage.OutputTokens,
	}, nil
}

// --- internal wire types ---

type apiRequest struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	System    string       `json:"system,omitempty"`
	Messages  []apiMessage `json:"messages"`
}

type apiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type apiResponse struct {
	Content []struct {
		Text string `json:"text"`
		Type string `json:"type"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}
