package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func okBody(text string) string {
	return fmt.Sprintf(`{
		"id":"msg_test","type":"message","role":"assistant",
		"content":[{"type":"text","text":%q}],
		"model":"claude-haiku-4-5",
		"usage":{"input_tokens":10,"output_tokens":5}
	}`, text)
}

func TestClaudeClient_Complete(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		ctxFn       func() (context.Context, context.CancelFunc)
		wantContent string
		wantErr     bool
	}{
		{
			name: "valid response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, okBody("hello"))
			},
			wantContent: "hello",
		},
		{
			name: "non-200 with error body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]string{"type": "invalid_request", "message": "bad request"},
				})
			},
			wantErr: true,
		},
		{
			name: "malformed json",
			handler: func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, "not json")
			},
			wantErr: true,
		},
		{
			name: "empty content array",
			handler: func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"content":[],"usage":{"input_tokens":0,"output_tokens":0}}`)
			},
			wantErr: true,
		},
		{
			name: "context already cancelled",
			handler: func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(50 * time.Millisecond)
				fmt.Fprint(w, okBody("late"))
			},
			ctxFn: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel() // pre-cancel
				return ctx, cancel
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			client := NewClaudeClient("test-key",
				WithBaseURL(srv.URL),
				WithTimeout(2*time.Second),
			)

			ctx := context.Background()
			var cancel context.CancelFunc
			if tt.ctxFn != nil {
				ctx, cancel = tt.ctxFn()
				defer cancel()
			}

			got, err := client.Complete(ctx, CompletionRequest{
				UserMessage: "test",
				MaxTokens:   50,
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr=%v got err=%v", tt.wantErr, err)
			}
			if !tt.wantErr && got.Content != tt.wantContent {
				t.Fatalf("want content %q got %q", tt.wantContent, got.Content)
			}
		})
	}
}

func TestClaudeClient_RequestFormat(t *testing.T) {
	var captured apiRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)
		fmt.Fprint(w, okBody("ok"))
	}))
	defer srv.Close()

	client := NewClaudeClient("key", WithBaseURL(srv.URL))
	client.Complete(context.Background(), CompletionRequest{
		SystemPrompt: "sys",
		UserMessage:  "user",
		MaxTokens:    42,
		Temperature:  0.5,
	})

	if captured.System != "sys" {
		t.Errorf("system prompt not forwarded: %q", captured.System)
	}
	if captured.MaxTokens != 42 {
		t.Errorf("max_tokens not forwarded: %d", captured.MaxTokens)
	}
	if len(captured.Messages) != 1 || captured.Messages[0].Content != "user" {
		t.Errorf("user message not forwarded: %+v", captured.Messages)
	}
}
