package api

import (
	"net/http"
	"strconv"

	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

func (s *APIServer) queryAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))

	records := s.auditRing.Query(store.AuditFilter{
		Decision:  q.Get("decision"),
		ToolName:  q.Get("tool"),
		SessionID: q.Get("session_id"),
		Limit:     limit,
	})

	if records == nil {
		records = []store.AuditRecord{}
	}
	writeJSON(w, http.StatusOK, records)
}
