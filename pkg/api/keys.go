package api

import (
	"encoding/json"
	"io"
	"net/http"
)

// GET /api/keys — list active key labels and creation times.
// Key material is never included in the response.
func (s *APIServer) listKeys(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.keyStore.List())
}

// POST /api/keys/rotate — rotate the key for a label.
// The new key is returned once in the response body; store it immediately.
// Request body (optional): {"label":"my-label"}  — defaults to "default".
func (s *APIServer) rotateKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label string `json:"label"`
	}
	if b, err := io.ReadAll(io.LimitReader(r.Body, 4*1024)); err == nil && len(b) > 0 {
		json.Unmarshal(b, &req) // ignore parse errors — label defaults to ""
	}
	if req.Label == "" {
		req.Label = "default"
	}

	newKey, err := s.keyStore.Rotate(req.Label)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "key generation failed")
		return
	}

	type rotateResponse struct {
		Key       string `json:"key"`
		Label     string `json:"label"`
		CreatedAt string `json:"created_at"`
		Warning   string `json:"warning"`
	}
	writeJSON(w, http.StatusOK, rotateResponse{
		Key:     newKey,
		Label:   req.Label,
		Warning: "store this key immediately — it will not be shown again",
	})
}
