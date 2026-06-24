package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/llm"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

// QueryRequest is the body for POST /api/query.
type QueryRequest struct {
	Question string `json:"question"`
}

// QueryResponse is the answer from the LLM governance assistant.
type QueryResponse struct {
	Answer       string `json:"answer"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	DurationMs   int64  `json:"duration_ms"`
}

const governanceSystemPrompt = `You are a governance assistant for PerchGuard, an AI agent admission control system.
You have access to recent audit decisions and active session data. Answer the operator's question
concisely and accurately based only on the data provided. Speak in terms of agent sessions,
tool calls, governance decisions (ALLOW/DENY/MUTATE/HUMAN_REVIEW/TERMINATE), and risk scores.`

func (s *APIServer) queryLLM(w http.ResponseWriter, r *http.Request) {
	if s.llmClient == nil {
		writeError(w, http.StatusServiceUnavailable, "LLM not configured")
		return
	}

	var req QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Question == "" {
		writeError(w, http.StatusBadRequest, "question is required")
		return
	}

	// Build context: last 50 audit records + all active sessions.
	records := s.auditRing.Query(store.AuditFilter{Limit: 50})
	sessions := s.sessions.List()

	contextData, err := json.Marshal(map[string]any{
		"recent_decisions": records,
		"active_sessions":  sessions,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "context build error")
		return
	}

	// Cap context at ~3000 chars to stay within Haiku's token budget.
	ctxStr := string(contextData)
	if len(ctxStr) > 3000 {
		ctxStr = ctxStr[:3000] + "…"
	}

	userMessage := fmt.Sprintf("Context:\n%s\n\nQuestion: %s", ctxStr, req.Question)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := s.llmClient.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: governanceSystemPrompt,
		UserMessage:  userMessage,
		MaxTokens:    512,
		Temperature:  0.2,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LLM query failed")
		return
	}

	writeJSON(w, http.StatusOK, QueryResponse{
		Answer:       resp.Content,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		DurationMs:   time.Since(start).Milliseconds(),
	})
}
