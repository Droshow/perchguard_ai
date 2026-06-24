package validator

import (
	"context"
	"fmt"
	"strings"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

// LineageValidator tracks per-session data provenance and escalates to HUMAN_REVIEW
// when a call consumes tracked session data refs and routes to an external destination.
//
// Detection scenario: agent reads a file (produces ref A), summarises it (consumes A,
// produces ref B), then emails the summary (consumes B → external dest). Each individual
// call may be ALLOW; the chain is flagged only on the third call when the lineage check
// sees a tracked ref heading to an external boundary.
//
// DataRefsIn is opt-in — callers without lineage instrumentation are unaffected.
type LineageValidator struct {
	store *store.LineageStore
}

func NewLineageValidator(ls *store.LineageStore) *LineageValidator {
	return &LineageValidator{store: ls}
}

func (v *LineageValidator) Name() string { return "lineage" }

func (v *LineageValidator) Validate(_ context.Context, req *admission.ToolCallAdmissionRequest) *admission.PolicyViolation {
	outRef := store.LineageRef(req.SessionID, req.UID)

	var violation *admission.PolicyViolation
	if len(req.DataRefsIn) > 0 && isExternalDest(req) {
		// Check if any consumed ref is known to this session's lineage graph.
		// If so, tracked data is being routed out of the boundary — escalate.
		if g := v.store.Graph(req.SessionID); g != nil && g.HasAny(req.DataRefsIn) {
			violation = &admission.PolicyViolation{
				Layer:  "validation",
				Policy: "lineage.crossBoundaryFlow",
				Detail: fmt.Sprintf(
					"tool %q routes data from tracked session refs to an external destination",
					req.ToolCall.Name,
				),
				Severity: "high",
				Decision: admission.DecisionHumanReview,
			}
		}
	}

	// Always record the edge — forensics need the full attempted flow, not just violations.
	v.store.AddEdge(req.SessionID, outRef, req.DataRefsIn, req.ToolCall.Name)
	return violation
}

// isExternalDest reports whether the tool call targets a destination outside the boundary.
// Checks the explicit DestinationURL field and common exfiltration tool name patterns.
func isExternalDest(req *admission.ToolCallAdmissionRequest) bool {
	if req.ToolCall.DestinationURL != "" {
		return true
	}
	name := strings.ToLower(req.ToolCall.Name)
	for _, kw := range []string{"send_email", "email_", "http_post", "upload", "send_", "export_", "webhook", "ftp", "rsync", "scp"} {
		if strings.Contains(name, kw) {
			return true
		}
	}
	return false
}
