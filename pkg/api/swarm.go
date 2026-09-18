package api

import (
	"net/http"
	"time"
)

// swarmNode is one governed agent session in the fleet-wide graph.
type swarmNode struct {
	SessionID       string    `json:"session_id"`
	AgentID         string    `json:"agent_id,omitempty"`
	RiskScore       float64   `json:"risk_score"`
	ParentSessionID string    `json:"parent_session_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// swarmEdge is either a delegation edge (parent registered a sub-agent session) or a
// data edge (a DataRefOut value flowed from one session's tool call into another's,
// possibly across the delegation boundary — see pkg/store.LineageStore.SeedInherited).
type swarmEdge struct {
	Type        string `json:"type"` // "delegation" | "data"
	From        string `json:"from,omitempty"`
	To          string `json:"to,omitempty"`
	FromSession string `json:"from_session,omitempty"`
	FromTool    string `json:"from_tool,omitempty"`
	ToSession   string `json:"to_session,omitempty"`
	ToTool      string `json:"to_tool,omitempty"`
	Ref         string `json:"ref,omitempty"`
}

type swarmGraph struct {
	Nodes []swarmNode `json:"nodes"`
	Edges []swarmEdge `json:"edges"`
}

// getSwarmGraph returns the live fleet as a graph: one node per active session (with
// its current risk score) plus delegation edges (from SessionState.ParentSessionID)
// and cross-session data-lineage edges (from the LineageStore). Built fresh from
// existing stores on every call — no new persisted state, safe to poll from a dashboard.
func (s *APIServer) getSwarmGraph(w http.ResponseWriter, r *http.Request) {
	sessions := s.sessions.List()

	nodes := make([]swarmNode, 0, len(sessions))
	edges := make([]swarmEdge, 0)

	for _, ss := range sessions {
		nodes = append(nodes, swarmNode{
			SessionID:       ss.SessionID,
			AgentID:         ss.ManifestID,
			RiskScore:       ss.RiskScore,
			ParentSessionID: ss.ParentSessionID,
			CreatedAt:       ss.CreatedAt,
			UpdatedAt:       ss.UpdatedAt,
		})
		if ss.ParentSessionID != "" {
			edges = append(edges, swarmEdge{Type: "delegation", From: ss.ParentSessionID, To: ss.SessionID})
		}

		if s.lineageStore == nil {
			continue
		}
		g := s.lineageStore.Graph(ss.SessionID)
		if g == nil {
			continue
		}
		for _, e := range g.Snapshot() {
			if e.InRefOrigin == "" || e.InRefOrigin == ss.SessionID {
				continue // produced and consumed in the same session — not a cross-agent edge
			}
			fromTool := ""
			if origin := s.lineageStore.Graph(e.InRefOrigin); origin != nil {
				if t, _, ok := origin.Producer(e.InRef); ok {
					fromTool = t
				}
			}
			edges = append(edges, swarmEdge{
				Type:        "data",
				FromSession: e.InRefOrigin,
				FromTool:    fromTool,
				ToSession:   ss.SessionID,
				ToTool:      e.Tool,
				Ref:         e.InRef,
			})
		}
	}

	writeJSON(w, http.StatusOK, swarmGraph{Nodes: nodes, Edges: edges})
}
