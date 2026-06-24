package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const defaultOllamaModel = "llama3"

type ollamaClient struct {
	baseURL    string
	model      string
	httpClient *http.Client
}

// OllamaOption configures the Ollama client.
type OllamaOption func(*ollamaClient)

func WithOllamaModel(model string) OllamaOption {
	return func(c *ollamaClient) { c.model = model }
}

func WithOllamaTimeout(d time.Duration) OllamaOption {
	return func(c *ollamaClient) { c.httpClient.Timeout = d }
}

// NewOllamaClient returns a Client backed by a local Ollama instance.
// baseURL is typically "http://localhost:11434".
func NewOllamaClient(baseURL string, opts ...OllamaOption) Client {
	c := &ollamaClient{
		baseURL:    baseURL,
		model:      defaultOllamaModel,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *ollamaClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	msgs := make([]ollamaMsg, 0, 2)
	if req.SystemPrompt != "" {
		msgs = append(msgs, ollamaMsg{Role: "system", Content: req.SystemPrompt})
	}
	msgs = append(msgs, ollamaMsg{Role: "user", Content: req.UserMessage})

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 150
	}

	body, err := json.Marshal(ollamaChatReq{
		Model:    c.model,
		Messages: msgs,
		Stream:   false,
		Options:  ollamaOpts{NumPredict: maxTokens, Temperature: req.Temperature},
	})
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("ollama: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("ollama: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("ollama: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return CompletionResponse{}, fmt.Errorf("ollama: status %d", resp.StatusCode)
	}

	var result ollamaChatResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return CompletionResponse{}, fmt.Errorf("ollama: decode: %w", err)
	}

	return CompletionResponse{
		Content:      result.Message.Content,
		InputTokens:  result.PromptEvalCount,
		OutputTokens: result.EvalCount,
	}, nil
}

// --- internal wire types ---

type ollamaChatReq struct {
	Model    string      `json:"model"`
	Messages []ollamaMsg `json:"messages"`
	Stream   bool        `json:"stream"`
	Options  ollamaOpts  `json:"options"`
}

type ollamaMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaOpts struct {
	NumPredict  int     `json:"num_predict,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
}

type ollamaChatResp struct {
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	PromptEvalCount int `json:"prompt_eval_count"`
	EvalCount       int `json:"eval_count"`
}
